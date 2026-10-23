#!/usr/bin/env node
/**
 * Explicit, disposable Linux/systemd integration check. The caller supplies an
 * App checkout (whose production remote.py is copied unchanged), a ShellBeam
 * Server Linux binary, and the fork candidate. No App checkout file is edited.
 *
 * node scripts/managed-lifecycle-smoke.mjs --app-checkout /path/shellbeam-app \
 *   --server-binary /path/shellbeam-server-linux-arm64 \
 *   --candidate-binary /path/shellbeam-monitoring-agent-linux-arm64 \
 *   --report /tmp/managed-lifecycle.json [--scope system|user]
 *
 * Requires Docker and the App's scripts/fixtures/probe-transfer image, normally
 * built with `docker build -t shellbeam-probe-transfer-fixture:local <directory>`.
 * The only privileged operations occur in a new disposable container. Original
 * 1.5.11 bytes are checked against the App's reviewed architecture/size/SHA pins.
 * Candidate bytes are explicitly supplied and locally test-signed; this checks
 * the publisher's artifact signature and the real production helper boundary,
 * not a production trusted catalog, native App IPC, SSH, or cross-machine move.
 * No production signing key/feed is read, written, replaced, or bypassed.
 */
import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { createHash, generateKeyPairSync, randomBytes, randomUUID, verify } from 'node:crypto'
import { mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { dirname, join, resolve } from 'node:path'
import { pathToFileURL } from 'node:url'

const options = {}
for (let i = 2; i < process.argv.length; i += 2) {
  const key = process.argv[i]
  if (!['--app-checkout', '--server-binary', '--candidate-binary', '--official-binary', '--report', '--image', '--scope'].includes(key) || !process.argv[i + 1] || options[key]) throw new Error('invalid_arguments')
  options[key] = process.argv[i + 1]
}
for (const key of ['--app-checkout', '--server-binary', '--candidate-binary', '--report']) if (!options[key]) throw new Error('explicit_inputs_required')
const app = resolve(options['--app-checkout'])
const scope = options['--scope'] ?? 'system'
assert.ok(['system', 'user'].includes(scope), 'invalid_service_scope')
const reportPath = resolve(options['--report'])
const fixtureImage = options['--image'] ?? 'shellbeam-probe-transfer-fixture:local'
const hash = bytes => createHash('sha256').update(bytes).digest('hex')
const delay = ms => new Promise(done => setTimeout(done, ms))
const run = (command, args, input, timeout = 30000) => execFileSync(command, args, { input, timeout, encoding: 'utf8', stdio: ['pipe', 'pipe', 'pipe'], maxBuffer: 64 * 1024 * 1024 }).trim()
const docker = (...args) => run('docker', args)
async function waitFor(callback, timeout = 60000) {
  const deadline = Date.now() + timeout
  while (Date.now() < deadline) {
    try { const result = await callback(); if (result) return result } catch { /* bounded read-only readiness */ }
    await delay(300)
  }
  throw new Error('bounded_read_timeout')
}
function architecture(bytes) {
  assert.equal(bytes.subarray(0, 4).toString('hex'), '7f454c46', 'linux_elf_required')
  assert.equal(bytes[4], 2, '64_bit_elf_required')
  assert.equal(bytes[5], 1, 'little_endian_elf_required')
  const arch = { 62: 'amd64', 183: 'arm64' }[bytes.readUInt16LE(18)]
  assert.ok(arch, 'supported_elf_architecture_required')
  return arch
}
const prefix = 'sb-managed-' + randomBytes(5).toString('hex')
const name = prefix + '-machine'
const deployment = randomUUID()
const unit = 'shellbeam-komari-' + deployment + '.service'
const report = {
  result: 'running', serviceScope: scope,
  coverage: ['production-helper-unchanged', 'real-systemd', 'real-agent-reports', 'exact-process-sha256'],
  limits: ['local-test-signature-only', 'no-native-app-ipc', 'no-ssh-route', 'no-cross-machine-transfer', 'no-production-feed-publication'],
  checks: [], freshReports: [], cleanup: false, serverStopped: false,
}
let stage = 'validate explicit input binaries'
let containerCreated = false
let temp
let remote
let inspect
let mutate
let managedRoot
let commandPrefix
try {
  const candidateBytes = await readFile(resolve(options['--candidate-binary']))
  const serverBytes = await readFile(resolve(options['--server-binary']))
  const arch = architecture(candidateBytes)
  assert.equal(architecture(serverBytes), arch, 'server_agent_architecture_mismatch')
  report.architecture = arch
  report.candidate = { version: '1.5.12', sha256: hash(candidateBytes), sizeBytes: candidateBytes.length }
  report.server = { sha256: hash(serverBytes), sizeBytes: serverBytes.length }
  const helper = await readFile(join(app, 'src-tauri/src/data/komari_agent/remote.py'))
  report.helperSha256 = hash(helper)
  const reviewed = JSON.parse(await readFile(join(app, 'releases/compatible-probe-pins.json'))).versions['1.5.11'][arch]
  const { downloadPinned } = await import(pathToFileURL(join(app, 'scripts/prepare-komari-agent-release.mjs')))
  const { canonicalCompatibleAgent, signCompatibleAgent } = await import(pathToFileURL(join(app, 'scripts/publish-komari-agent.mjs')))
  stage = 'verify reviewed original 1.5.11 bytes'
  const originalUrl = `https://github.com/komari-monitor/komari-agent/releases/download/1.5.11/komari-agent-linux-${arch}`
  const originalBytes = options['--official-binary'] ? await readFile(resolve(options['--official-binary'])) : await downloadPinned(originalUrl, reviewed)
  assert.equal(originalBytes.length, reviewed.size, 'original_size_mismatch')
  assert.equal(hash(originalBytes), reviewed.sha256, 'original_sha256_mismatch')
  assert.equal(architecture(originalBytes), arch, 'original_architecture_mismatch')
  const keys = generateKeyPairSync('ed25519')
  const privatePem = keys.privateKey.export({ type: 'pkcs8', format: 'pem' })
  const makeArtifact = (bytes, version, url) => {
    const manifest = signCompatibleAgent({ bytes, version, architecture: arch, url, expectedSha256: hash(bytes), expectedSize: bytes.length, privateKey: privatePem })
    assert.equal(verify(null, canonicalCompatibleAgent(manifest), keys.publicKey, Buffer.from(manifest.signature, 'base64')), true, 'local_artifact_signature_invalid')
    return { bytes, manifest }
  }
  const original = makeArtifact(originalBytes, '1.5.11', originalUrl)
  const candidate = makeArtifact(candidateBytes, '1.5.12', `https://github.com/0xJean/shellbeam-monitoring-agent/releases/download/v1.5.12/shellbeam-monitoring-agent-linux-${arch}`)
  report.original = { version: original.manifest.version, sha256: original.manifest.sha256, sizeBytes: originalBytes.length }
  report.artifactSignaturesVerified = true
  temp = await mkdtemp(join(tmpdir(), 'shellbeam-managed-smoke-'))
  const helperFile = join(temp, 'remote.py')
  const serverFile = join(temp, 'server')
  await writeFile(helperFile, helper, { mode: 0o600 })
  await writeFile(serverFile, serverBytes, { mode: 0o700 })
  stage = 'create isolated Linux systemd container'
  docker('network', 'create', prefix)
  docker('run', '-d', '--name', name, '--hostname', name, '--platform', `linux/${arch}`, '--network', prefix,
    '--privileged', '--cgroupns=private', '--tmpfs', '/run', '--tmpfs', '/run/lock',
    '--memory', '768m', '--cpus', '1', '--pids-limit', '512', '-p', '127.0.0.1::25700', fixtureImage)
  containerCreated = true
  await waitFor(() => docker('exec', name, 'systemctl', 'is-active', 'multi-user.target') === 'active')
  docker('exec', name, 'mkdir', '-p', '/opt/fixture')
  docker('cp', helperFile, name + ':/opt/fixture/remote.py')
  docker('cp', serverFile, name + ':/opt/fixture/server')
  docker('exec', name, 'chmod', '755', '/opt/fixture/server', '/opt/fixture/remote.py')
  const userId = docker('exec', name, 'id', '-u', scope === 'user' ? 'probe' : 'root')
  if (scope === 'user') {
    docker('exec', name, 'loginctl', 'enable-linger', 'probe')
    docker('exec', name, 'systemctl', 'start', `user@${userId}.service`)
  }
  const home = scope === 'user' ? '/home/probe' : '/root'
  commandPrefix = ['exec', '-i', '--user', scope === 'user' ? 'probe' : 'root', '--env', `HOME=${home}`]
  if (scope === 'user') commandPrefix.push('--env', `XDG_RUNTIME_DIR=/run/user/${userId}`, '--env', `DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/${userId}/bus`)
  commandPrefix.push(name)
  const systemctl = (...args) => docker(...commandPrefix, 'systemctl', ...(scope === 'user' ? ['--user'] : []), ...args)
  remote = async request => {
    const raw = run('docker', [...commandPrefix, 'python3', '-I', '/opt/fixture/remote.py'], JSON.stringify({ deployment, ...request }), 120000)
    const result = JSON.parse(raw)
    report.remoteChecks ??= []
    report.remoteChecks.push({ action: request.action, ok: result.ok, code: result.code, status: result.state?.status, active: result.state?.active })
    return result
  }
  inspect = async () => { const result = await remote({ action: 'inspect' }); assert.equal(result.ok, true, 'inspection_failed'); return result }
  let config
  mutate = async (action, artifact, bytesOverride) => {
    const baseline = await inspect()
    return remote({ action, identity: baseline.identity, expected: baseline.state, job: randomUUID(), release: artifact?.manifest, bytes: bytesOverride ?? artifact?.bytes?.toString('base64'), configuration: config })
  }
  const initial = await inspect()
  assert.equal(initial.state.status, 'absent', 'new_deployment_must_be_empty')
  managedRoot = initial.identity.root
  assert.equal(managedRoot, `${home}/.local/share/shellbeam/komari/${deployment}`, 'managed_root_changed')
  assert.equal(initial.identity.architecture, arch, 'container_architecture_mismatch')
  report.checks.push('unchanged-production-helper-and-managed-layout')
  stage = 'initialize supplied ShellBeam Server with synthetic credentials'
  docker('exec', '-d', name, '/opt/fixture/server', 'server', '--listen', '0.0.0.0:25700', '--database', '/opt/fixture/main.db')
  const local = 'http://127.0.0.1:' + docker('port', name, '25700').split(':').at(-1)
  const apiKey = randomBytes(32).toString('hex')
  const request = async (path, body, auth = { Authorization: `Bearer ${apiKey}` }) => {
    const response = await fetch(local + path, { method: body === undefined ? 'GET' : 'POST', headers: { ...auth, 'Content-Type': 'application/json' }, ...(body === undefined ? {} : { body: JSON.stringify(body) }), signal: AbortSignal.timeout(10000) })
    if (response.status !== 200) { report.transientHttpResponses ??= []; if (report.transientHttpResponses.length < 20) report.transientHttpResponses.push({ path, status: response.status }) }
    assert.equal(response.status, 200, 'server_http_failure')
    const value = await response.json()
    return { response, value, data: value?.data ?? value?.result ?? value }
  }
  stage = 'read isolated server installation state'
  await waitFor(async () => (await request('/api/install/status')).data?.required === true)
  const username = 'fixture'
  const password = 'Sb1' + randomBytes(24).toString('hex')
  stage = 'complete isolated server installation'
  await request('/api/install/complete', { username, password, sitename: 'Isolated managed lifecycle', metric_dsn: '/opt/fixture/metrics.db' })
  stage = 'read supplied server compatibility version'
  // Installation replaces first-run routes asynchronously; only bounded reads
  // retry here, never the installation POST or any lifecycle mutation.
  const serverVersion = await waitFor(async () => {
    const value = (await request('/api/version')).data
    return value?.version === '1.6.0' ? value : false
  })
  assert.equal(serverVersion.version, '1.6.0', 'server_compatible_version_mismatch')
  report.server.version = serverVersion.version
  report.server.product = serverVersion.product ?? serverVersion.name ?? null
  stage = 'authenticate isolated server administrator'
  const login = await request('/api/login', { username, password })
  const cookie = login.response.headers.get('set-cookie')?.split(';')[0]
  assert.ok(cookie, 'isolated_login_failed')
  stage = 'save isolated server API key'
  await request('/api/admin/settings', { api_key: apiKey, private_site: true, geo_ip_enabled: false, notification_enabled: false, eula_accepted: true }, { Cookie: cookie, Origin: local })
  stage = 'create isolated server monitoring node'
  const added = (await request('/api/admin/client/add', { name: 'managed-lifecycle-fixture' })).data
  const rpc = async method => (await request('/api/rpc2', { jsonrpc: '2.0', id: 1, method, params: { uuid: added.uuid } })).value.result
  const originalNode = await rpc('admin:getClient')
  const originalToken = (await rpc('admin:getClientToken')).token
  assert.equal(originalToken, added.token, 'initial_node_token_mismatch')
  config = { endpoint: 'http://127.0.0.1:25700', token: added.token, disable_auto_update: true, disable_web_ssh: true, interval: 5, info_report_interval: 1, month_rotate: 0, get_ip_addr_from_nic: true, include_nics: 'lo' }
  function originalSamples(value, output = []) {
    if (Array.isArray(value)) { for (const child of value) originalSamples(child, output); return output }
    if (!value || typeof value !== 'object') return output
    const time = Date.parse(value.time ?? value.updated_at ?? '')
    const cpu = typeof value.cpu === 'number' ? value.cpu : value.cpu?.usage
    if (Number.isFinite(time) && Number.isFinite(cpu)) output.push({ time, cpu })
    for (const key of ['data', 'records', 'reports', 'result']) if (value[key]) originalSamples(value[key], output)
    return output
  }
  const latest = async () => originalSamples((await request(`/api/recent/${added.uuid}`)).value).sort((a, b) => b.time - a.time)[0] ?? { time: 0 }
  const fileRead = path => docker(...commandPrefix, 'python3', '-I', '-c', 'import pathlib,sys;sys.stdout.write(pathlib.Path(sys.argv[1]).read_text())', path)
  const processIdentity = artifact => {
    const pid = systemctl('show', unit, '--property=MainPID', '--value')
    assert.match(pid, /^[1-9][0-9]*$/, 'active_main_pid_required')
    const raw = docker(...commandPrefix, 'python3', '-I', '-c',
      'import hashlib,json,os,pathlib,sys;p=pathlib.Path("/proc")/sys.argv[1];b=(p/"exe").read_bytes();print(json.dumps({"sha256":hashlib.sha256(b).hexdigest(),"executable":os.readlink(p/"exe"),"argv":(p/"cmdline").read_bytes().rstrip(b"\\0").decode().split("\\0"),"start":(p/"stat").read_text().rsplit(")",1)[1].split()[19]}))', pid)
    const found = JSON.parse(raw)
    assert.equal(found.sha256, artifact.manifest.sha256, 'actual_process_sha256_mismatch')
    assert.equal(found.executable, `${managedRoot}/releases/${artifact.manifest.sha256}/agent`, 'actual_process_path_mismatch')
    assert.deepEqual(found.argv, [`${managedRoot}/current/agent`, '--config', `${managedRoot}/config.json`], 'actual_process_argv_mismatch')
    return { pid, start: found.start, sha256: found.sha256 }
  }
  const fresh = async (artifact, before, started, label) => waitFor(async () => {
    const node = await rpc('admin:getClient')
    const sample = await latest()
    if (!(node.uuid === added.uuid && node.created_at === originalNode.created_at && node.version?.replace(/^v/, '') === artifact.manifest.version && sample.time > before && sample.time >= started - 5000 && sample.time <= Date.now() + 5000 && sample.time >= Date.now() - 60000)) return false
    const state = await inspect()
    if (!(state.state.active && state.state.current?.sha256 === artifact.manifest.sha256)) return false
    const process = processIdentity(artifact)
    report.freshReports.push({ label, version: artifact.manifest.version, originalSampleAt: sample.time, cpu: sample.cpu, before, started, verifiedAt: Date.now(), processSha256: process.sha256, nodeIdentityVerified: true })
    return true
  })
  const operateAndVerify = async (action, artifact, label, includeBytes = true) => {
    stage = label
    const before = (await latest()).time
    const started = Date.now()
    const result = await mutate(action, includeBytes ? artifact : { manifest: artifact.manifest })
    assert.equal(result.ok, true, 'managed_mutation_failed')
    assert.equal(result.state.active, true, 'managed_service_not_active')
    await fresh(artifact, before, started, label)
    report.checks.push(label)
    return result
  }
  await operateAndVerify('install', original, 'original-1.5.11-install-with-fresh-sample')
  const configText = fileRead(`${managedRoot}/config.json`)
  const configMetadata = JSON.parse(docker(...commandPrefix, 'python3', '-I', '-c', 'import json,pathlib,sys;p=pathlib.Path(sys.argv[1]);print(json.dumps({"mode":p.stat().st_mode&511,"sha256":__import__("hashlib").sha256(p.read_bytes()).hexdigest()}))', `${managedRoot}/config.json`))
  assert.equal(configMetadata.mode, 0o600, 'configuration_permissions_changed')
  report.configurationSha256 = configMetadata.sha256
  stage = 'reject changed candidate before activation'
  const unchanged = await inspect()
  const bad = await mutate('upgrade', candidate, Buffer.from('altered').toString('base64'))
  assert.equal(bad.ok, false, 'tampered_candidate_not_rejected')
  assert.equal(bad.code, 'artifact_hash_mismatch', 'tampered_candidate_wrong_error')
  assert.deepEqual(await inspect(), unchanged, 'tampered_candidate_changed_remote_state')
  report.checks.push('tampered-candidate-rejected-without-state-change')
  await operateAndVerify('upgrade', candidate, 'fork-1.5.12-upgrade-with-fresh-sample')
  assert.equal(fileRead(`${managedRoot}/config.json`), configText, 'upgrade_changed_configuration')
  await operateAndVerify('rollback', original, 'retained-original-rollback-without-download', false)
  assert.equal(fileRead(`${managedRoot}/config.json`), configText, 'rollback_changed_configuration')
  await operateAndVerify('upgrade', candidate, 'fork-second-upgrade-after-rollback')
  stage = 'real systemd restart preserves exact managed identity'
  const oldProcess = processIdentity(candidate)
  const beforeRestart = (await latest()).time
  const restartStarted = Date.now()
  systemctl('restart', unit)
  await fresh(candidate, beforeRestart, restartStarted, 'fork-restart-with-fresh-sample')
  const restarted = processIdentity(candidate)
  assert.notEqual(`${restarted.pid}:${restarted.start}`, `${oldProcess.pid}:${oldProcess.start}`, 'restart_did_not_change_process_incarnation')
  report.checks.push('fork-restart-with-fresh-sample')
  stage = 'cross-process inspect is read-only and does not replay'
  const savedState = fileRead(`${managedRoot}/state.json`)
  await inspect(); await inspect()
  assert.equal(fileRead(`${managedRoot}/state.json`), savedState, 'inspection_changed_state')
  report.checks.push('cross-process-read-only-inspection')
  stage = 'uninstall retains node token configuration releases and history'
  const beforeRemove = (await latest()).time
  const removed = await mutate('uninstall')
  assert.equal(removed.ok, true, 'managed_uninstall_failed')
  assert.equal(removed.state.status, 'retired', 'uninstall_not_retired')
  assert.equal(removed.state.active, false, 'uninstall_process_still_active')
  assert.equal(fileRead(`${managedRoot}/config.json`), configText, 'uninstall_removed_configuration')
  const retainedHashes = JSON.parse(docker(...commandPrefix, 'python3', '-I', '-c', 'import json,pathlib,sys;print(json.dumps(sorted(p.name for p in pathlib.Path(sys.argv[1]).iterdir())))', `${managedRoot}/releases`))
  assert.ok(retainedHashes.includes(original.manifest.sha256) && retainedHashes.includes(candidate.manifest.sha256), 'uninstall_removed_retained_artifacts')
  assert.equal((await rpc('admin:getClientToken')).token, originalToken, 'uninstall_changed_node_token')
  assert.equal((await rpc('admin:getClient')).created_at, originalNode.created_at, 'uninstall_changed_node_identity')
  assert.ok((await latest()).time >= beforeRemove, 'uninstall_removed_recent_history')
  const afterRemove = await inspect()
  assert.equal(afterRemove.state.active, false, 'readback_found_running_agent')
  report.checks.push('uninstall-preserves-node-token-configuration-artifacts-and-history')
  await operateAndVerify('install', candidate, 'fork-reinstall-retired-deployment-with-fresh-sample')
  assert.equal(fileRead(`${managedRoot}/config.json`), configText, 'reinstall_changed_configuration')
  stage = 'final uninstall and readback after reinstall'
  const finalRemoved = await mutate('uninstall')
  assert.equal(finalRemoved.ok, true, 'final_managed_uninstall_failed')
  assert.equal(finalRemoved.state.status, 'retired', 'final_uninstall_not_retired')
  assert.equal((await inspect()).state.active, false, 'final_uninstall_process_still_active')
  assert.equal((await rpc('admin:getClientToken')).token, originalToken, 'final_uninstall_changed_node_token')
  report.checks.push('final-uninstall-after-reinstall')
  report.result = 'passed'
} catch (error) {
  report.result = 'failed'
  report.failedStage = stage
  const firstLine = error instanceof Error ? error.message.split('\n')[0] : ''
  if (/^[a-z0-9_-]+$/i.test(firstLine)) report.failureCode = firstLine
  process.exitCode = 1
} finally {
  if (containerCreated) {
    try {
      if (inspect && mutate) {
        const state = await inspect()
        if (!['absent', 'retired', 'released'].includes(state.state.status)) {
          const removed = await mutate('uninstall')
          assert.equal(removed.ok, true, 'cleanup_uninstall_failed')
        }
      }
    } catch { report.cleanupHelperFailed = true }
    try { docker('rm', '-f', name); containerCreated = false; report.serverStopped = true } catch { report.cleanup = false }
  }
  try { docker('network', 'rm', prefix) } catch { /* network creation may not have completed */ }
  if (!containerCreated) {
    report.cleanup = true
    if (temp) await rm(temp, { recursive: true, force: true })
  }
  if (!report.cleanup || !report.serverStopped) { report.result = 'failed'; process.exitCode = 1 }
  await mkdir(dirname(reportPath), { recursive: true })
  await writeFile(reportPath, JSON.stringify(report, null, 2) + '\n', { mode: 0o600 })
  console.log(JSON.stringify(report))
}
