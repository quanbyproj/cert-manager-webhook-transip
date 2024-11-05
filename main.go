package main

import (
	"github.com/jetstack/cert-manager/pkg/issuer/acme/dns/util"
	"encoding/json"
	"context"
	"strings"
	"bytes"
	"fmt"
	"os"

	extapi "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"
	"github.com/cert-manager/cert-manager/pkg/acme/webhook/cmd"
	"github.com/transip/gotransip/v6"
	"github.com/transip/gotransip/v6/domain"
	"github.com/transip/gotransip/v6/repository"
)

var GroupName = os.Getenv("GROUP_NAME")

func main() {
	if GroupName == "" {
		panic("GROUP_NAME must be specified")
	}

	// This will register our custom DNS provider with the webhook serving
	// library, making it available as an API under the provided GroupName.
	// You can register multiple DNS provider implementations with a single
	// webhook, where the Name() method will be used to disambiguate between
	// the different implementations.
	cmd.RunWebhookServer(GroupName,
		&transipDNSProviderSolver{},
	)
}

// transipDNSProviderSolver implements the provider-specific logic needed to
// 'present' an ACME challenge TXT record for the TransIP DNS provider.
type transipDNSProviderSolver struct {
	client *kubernetes.Clientset
}

// transipDNSProviderConfig is a structure that is used to decode into when
// solving a DNS01 challenge.
// This information is provided by cert-manager, and may be a reference to
// additional configuration that's needed to solve the challenge for this
// particular certificate or issuer.
// This typically includes references to Secret resources containing DNS
// provider credentials, in cases where a 'multi-tenant' DNS solver is being
// created.
// If you do *not* require per-issuer or per-certificate configuration to be
// provided to your webhook, you can skip decoding altogether in favour of
// using CLI flags or similar to provide configuration.
// You should not include sensitive information here. If credentials need to
// be used by your provider here, you should reference a Kubernetes Secret
// resource and fetch these credentials using a Kubernetes clientset.
type transipDNSProviderConfig struct {
	AccountName         string               `json:"accountName"`
	PrivateKey          []byte               `json:"privateKey"`
	PrivateKeySecretRef v1.SecretKeySelector `json:"privateKeySecretRef"`
	TTL                 int                  `json:"ttl"`
}

// Name is used as the name for this DNS solver when referencing it on the ACME
// Issuer resource.
// This should be unique **within the group name**, i.e. you can have two
// solvers configured with the same Name() **so long as they do not co-exist
// within a single webhook deployment**.
// For example, `cloudflare` may be used as the name of a solver.
func (c *transipDNSProviderSolver) Name() string {
	return "transip"
}

func (c *transipDNSProviderSolver) NewTransipClient(ch *v1alpha1.ChallengeRequest, cfg *transipDNSProviderConfig) (*repository.Client, error) {
	privateKey := cfg.PrivateKey

	if len(privateKey) == 0 {
		secret, err := c.client.CoreV1().Secrets(ch.ResourceNamespace).Get(context.TODO(), cfg.PrivateKeySecretRef.Name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}

		ok := false
		privateKey, ok = secret.Data[cfg.PrivateKeySecretRef.Key]
		if !ok {
			return nil, fmt.Errorf("no private key for %q in secret '%s/%s'", cfg.PrivateKeySecretRef.Name, cfg.PrivateKeySecretRef.Key, ch.ResourceNamespace)
		}
	}

	fmt.Printf("creating SOAP client ...\n")

	client, err := gotransip.NewClient(gotransip.ClientConfiguration{
		AccountName:      cfg.AccountName,
		PrivateKeyReader: bytes.NewReader(privateKey),
	})
	if err != nil {
		return nil, err
	}

	return &client, nil
}

func (c *transipDNSProviderSolver) NewDNSEntryFromChallenge(ch *v1alpha1.ChallengeRequest, cfg *transipDNSProviderConfig, domainName string) domain.DNSEntry {
	return domain.DNSEntry{
		Name:    extractRecordName(ch.ResolvedFQDN, domainName),
		Expire:  cfg.TTL,
		Type:    "TXT",
		Content: ch.Key,
	}
}

// Present is responsible for actually presenting the DNS record with the
// DNS provider.
// This method should tolerate being called multiple times with the same value.
// cert-manager itself will later perform a self check to ensure that the
// solver has correctly configured the DNS provider.
func (c *transipDNSProviderSolver) Present(ch *v1alpha1.ChallengeRequest) error {
	domainName := extractDomainName(ch.ResolvedZone)
	cfg, err := loadConfig(ch.Config)
	if err != nil {
		fmt.Printf("Error while loading config: %s\n", err)
		return err
	}

	client, err := c.NewTransipClient(ch, cfg)
	if err != nil {
		fmt.Printf("Error while creating SOAP client: %s\n", err)
		return err
	}

	fmt.Printf("presenting record for %s (%s)\n", ch.ResolvedFQDN, domainName)

	domainRepo := domain.Repository{Client: *client}
	dnsEntries, err := domainRepo.GetDNSEntries(domainName)
	if err != nil {
		fmt.Printf("Error while getting domain info for %s: %s\n", domainName, err)
		return err
	}

	acmeDnsEntry := c.NewDNSEntryFromChallenge(ch, cfg, domainName)

	// This method should tolerate being called multiple times
	// with the same value. If a TXT record for this request
	// already exists, we'll simply exit.
	for _, s := range dnsEntries {
		if s == acmeDnsEntry {
			fmt.Printf("ACME DNS entry already exists, skip\n")
			return nil
		}
	}

	err = domainRepo.AddDNSEntry(domainName, acmeDnsEntry)
	if err != nil {
		fmt.Printf("Error while setting DNS entries for domain %s: %s\n", domainName, err)
		return err
	}

	fmt.Printf("new record has been set %v", acmeDnsEntry)

	return nil
}

// CleanUp should delete the relevant TXT record from the DNS provider console.
// If multiple TXT records exist with the same record name (e.g.
// _acme-challenge.example.com) then **only** the record with the same `key`
// value provided on the ChallengeRequest should be cleaned up.
// This is in order to facilitate multiple DNS validations for the same domain
// concurrently.
func (c *transipDNSProviderSolver) CleanUp(ch *v1alpha1.ChallengeRequest) error {
	domainName := extractDomainName(ch.ResolvedZone)

	cfg, err := loadConfig(ch.Config)
	if err != nil {
		return err
	}

	client, err := c.NewTransipClient(ch, cfg)
	if err != nil {
		return err
	}

	fmt.Printf("cleaning up record for %s (%s)", ch.ResolvedFQDN, domainName)

	domainRepo := domain.Repository{Client: *client}
	dnsEntries, err := domainRepo.GetDNSEntries(domainName)
	if err != nil {
		return err
	}

	acmeDnsEntry := c.NewDNSEntryFromChallenge(ch, cfg, domainName)

	// If multiple TXT records exist with the same record name (e.g.
	// _acme-challenge.example.com) then **only** the record with the same `key`
	// value provided on the ChallengeRequest should be cleaned up.

	for _, s := range dnsEntries {
		if s == acmeDnsEntry {
			fmt.Printf("deleting dns record %v", s)

			err = domainRepo.RemoveDNSEntry(domainName, acmeDnsEntry)
			if err != nil {
				return err
			}

			return nil
		}
	}

	fmt.Printf("did not find a dns record matching %v", acmeDnsEntry)

	return nil
}

// Initialize will be called when the webhook first starts.
// This method can be used to instantiate the webhook, i.e. initialising
// connections or warming up caches.
// Typically, the kubeClientConfig parameter is used to build a Kubernetes
// client that can be used to fetch resources from the Kubernetes API, e.g.
// Secret resources containing credentials used to authenticate with DNS
// provider accounts.
// The stopCh can be used to handle early termination of the webhook, in cases
// where a SIGTERM or similar signal is sent to the webhook process.
func (c *transipDNSProviderSolver) Initialize(kubeClientConfig *rest.Config, stopCh <-chan struct{}) error {
	cl, err := kubernetes.NewForConfig(kubeClientConfig)
	if err != nil {
		return err
	}

	c.client = cl

	///// END OF CODE TO MAKE KUBERNETES CLIENTSET AVAILABLE
	return nil
}

// loadConfig is a small helper function that decodes JSON configuration into
// the typed config struct.
func loadConfig(cfgJSON *extapi.JSON) (*transipDNSProviderConfig, error) {
	cfg := transipDNSProviderConfig{}
	// handle the 'base case' where no configuration has been provided
	if cfgJSON == nil {
		return &cfg, nil
	}
	if err := json.Unmarshal(cfgJSON.Raw, &cfg); err != nil {
		return &cfg, fmt.Errorf("error decoding solver config: %v", err)
	}

	return &cfg, nil
}

func extractRecordName(fqdn, domain string) string {
	if idx := strings.Index(fqdn, "."+domain); idx != -1 {
		return fqdn[:idx]
	}
	return util.UnFqdn(fqdn)
}

func extractDomainName(zone string) string {
	authZone, err := util.FindZoneByFqdn(context.TODO(), zone, util.RecursiveNameservers)
	if err != nil {
		fmt.Printf("could not get zone by fqdn %v", err)
		return zone
	}
	return util.UnFqdn(authZone)
}
