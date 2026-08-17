package version

import (
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/component-base/metrics"
	"k8s.io/component-base/metrics/legacyregistry"
)

// Set via -ldflags -X during build (see Makefile LDFLAGS).
var (
	buildVersion     string
	buildGitRevision string
	buildDate        string
)

// Get returns the overall codebase version. It's for detecting
// what code a binary was built from.
func Get() version.Info {
	return version.Info{
		GitCommit:  buildGitRevision,
		GitVersion: buildVersion,
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
	buildInfo.WithLabelValues(buildVersion, buildGitRevision, buildDate).Set(1)

	legacyregistry.MustRegister(buildInfo)
}
