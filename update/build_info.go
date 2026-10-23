package update

// Provenance is deliberately separate from protocol/configuration fields. The
// App expects a numeric 1.5.x report version and the legacy komari runtime name.
type Provenance struct {
	Product         string `json:"product"`
	Version         string `json:"version"`
	Hash            string `json:"hash"`
	Repository      string `json:"repository"`
	Runtime         string `json:"runtime"`
	UpstreamVersion string `json:"upstream_version"`
	UpstreamCommit  string `json:"upstream_commit"`
}

func BuildInfo() Provenance {
	return Provenance{
		Product: ProductName, Version: CurrentVersion, Hash: VersionHash,
		Repository: Repo, Runtime: "komari",
		UpstreamVersion: UpstreamVersion, UpstreamCommit: UpstreamCommit,
	}
}
