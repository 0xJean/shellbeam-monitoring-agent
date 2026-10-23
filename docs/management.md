# Managing ShellBeam Monitoring Agent

Use the ShellBeam application's signed monitoring-probe controls for managed
Linux deployments. Status, start/stop, upgrades, rollback and removal must keep
the existing deployment's owner, binary hash and Server token checks.

The product can report ShellBeam Monitoring Agent while the managed runtime
identity remains `komari`. Do not rename an installed service or move its owned
directory during branding. See [the integration contract](integration.md).

## Uninstall

For an App-managed probe, select its deployment in ShellBeam and use the probe
removal action. The current manager stops/removes its exact owned systemd unit and marks the
deployment retired. It intentionally retains local metadata, configuration and
release files for recovery. Use only supported cleanup options for any further
deletion. Retain Server history or the node according to existing removal
options. Do not delete a directory shared by another deployment.

For a manually supervised foreground process, stop that process through the
same supervisor that started it, then remove only its own binary and private
configuration. For an existing standalone `komari-agent.service`, use the
original installation's service management/removal procedure after verifying
which executable, configuration and user it owns. This fork's `install.sh` and
`install.ps1` do not create services and cannot uninstall an unrelated install.

If remote control was enabled and access must be revoked immediately, disable
or remove that node's token on your Server as well. A local stop alone does not
revoke credentials copied elsewhere.
