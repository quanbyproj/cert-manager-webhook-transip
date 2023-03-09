package main

import (
	"os"
	"testing"

	"github.com/jetstack/cert-manager/test/acme/dns"

	"github.com/quanbylab/cert-manager-webhook-transip"
)

var (
	zone = os.Getenv("TEST_ZONE_NAME")
)

func TestRunsSuite(t *testing.T) {
	// The manifest path should contain a file named config.json that is a
	// snippet of valid configuration that should be included on the
	// ChallengeRequest passed as part of the test cases.
	//

	fixture := dns.NewFixture(&transipDNSProviderSolver{},
		dns.SetResolvedZone(zone),
		dns.SetAllowAmbientCredentials(false),
		dns.SetManifestPath("testdata/transip"),
		dns.SetBinariesPath("bin"),
	)
	//need to uncomment and  RunConformance delete runBasic and runExtended once https://github.com/jetstack/cert-manager/pull/4835 is merged
	//fixture.RunConformance(t)
	fixture.RunBasic(t)
	fixture.RunExtended(t)

}
