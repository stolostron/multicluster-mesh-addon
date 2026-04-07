package version

import (
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/component-base/metrics"
	"k8s.io/component-base/metrics/legacyregistry"
)

var (
	// commitFromGit is a constant representing the source version that
	// generated this build. It should be set during build via -ldflags.
	commitFromGit string
	// versionFromGit is a constant representing the version tag that
	// generated this build. It should be set during build via -ldflags.
	versionFromGit string
	// build date in ISO8601 format, output of $(date -u +'%Y-%m-%dT%H:%M:%SZ').
	buildDate string
)

// Get returns the overall codebase version. It's for detecting
// what code a binary was built from.
func Get() version.Info {
	return version.Info{
		GitCommit:  commitFromGit,
		GitVersion: versionFromGit,
		BuildDate:  buildDate,
	}
}

func init() {
	buildInfo := metrics.NewGaugeVec(
		&metrics.GaugeOpts{
			Name: "multicluster_mesh_addon_build_info",
			Help: "A metric with a constant '1' value labeled by git version, git commit & build date from which multicluster-mesh-addon was built.",
		},
		[]string{"gitVersion", "gitCommit", "buildDate"},
	)
	buildInfo.WithLabelValues(versionFromGit, commitFromGit, buildDate).Set(1)

	legacyregistry.MustRegister(buildInfo)
}
