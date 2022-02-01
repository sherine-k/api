package v1

import (
	"context"
	os "os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	clientschema "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/scheme"
	"k8s.io/apiextensions-apiserver/test/integration/fixtures"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/storage/etcd3/testserver"
)

// testserver uses its own pkgPath in order to use it
// as options.CustomResourceDefinitionsServerOptions.RecommendedOptions.SecureServing.ServerCert.FixtureDirectory
// so we need to create the testdata folder ahead of time
func prepareTestDataFolder() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	testdata := filepath.FromSlash(wd + "/../../vendor/k8s.io/apiextensions-apiserver/pkg/cmd/server/testing/testdata")
	err = os.Mkdir(testdata, 0755)
	if err != nil {
		return "", err
	}
	return testdata, nil
}
func TestDNSSchema(t *testing.T) {
	testdata, err := prepareTestDataFolder()
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(testdata)

	cfg := testserver.NewTestConfig(t)

	// Start etcd. it will be automatically stopped at the end of the test
	etcdClient := testserver.RunEtcd(t, cfg)

	// check at least 1 endpoint is up
	resp, err := etcdClient.Status(context.TODO(), etcdClient.Endpoints()[0])
	if err != nil && len(resp.Errors) > 0 {
		t.Fatal(err)
	}

	os.Setenv("KUBE_INTEGRATION_ETCD_URL", etcdClient.Endpoints()[0])

	// Read CRD from file
	crdInBytes, err := os.ReadFile("0000_70_dns-operator_00.crd.yaml")
	if err != nil {
		t.Fatal(err)
	}

	// Start an API client
	tearDown, apiExtensionClient, client, err := fixtures.StartDefaultServerWithClients(t)
	if err != nil {
		t.Fatal(err)
	}
	defer tearDown()

	// decode CRD manifest
	obj, _, err := clientschema.Codecs.UniversalDeserializer().Decode([]byte(crdInBytes), nil, nil)
	if err != nil {
		t.Fatalf("failed decoding of: %v\n\n%s", err, crdInBytes)
	}
	crd := obj.(*apiextensionsv1.CustomResourceDefinition)

	// create CRDs
	t.Logf("Creating CRD %s", crd.Name)
	if _, err = fixtures.CreateNewV1CustomResourceDefinition(crd, apiExtensionClient, client); err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}

	// create CR
	gvr, testCases := schema.GroupVersionResource{
		Group:    crd.Spec.Group,
		Version:  crd.Spec.Versions[0].Name,
		Resource: crd.Spec.Names.Plural,
	}, []struct {
		name                 string
		dns                  *DNS
		expectedErrorMessage string
		expectedType         UpstreamType
		expectedAddress      string
		expectedPort         uint32
	}{
		{
			name: "Dns spec without upstreamResolvers passes",
			dns: &DNS{
				TypeMeta: metav1.TypeMeta{
					Kind:       "DNS",
					APIVersion: "operator.openshift.io/v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "default",
				},
				Spec: DNSSpec{},
			},
			expectedErrorMessage: "",
			expectedType:         SystemResolveConfType,
		},
		{
			name: "Dns spec with upstream typed System passes",
			dns: &DNS{
				TypeMeta: metav1.TypeMeta{
					Kind:       "DNS",
					APIVersion: "operator.openshift.io/v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "default",
				},
				Spec: DNSSpec{
					UpstreamResolvers: UpstreamResolvers{
						Upstreams: []Upstream{
							{
								Type: SystemResolveConfType,
							},
						},
						Policy: RoundRobinForwardingPolicy,
					},
				},
			},
			expectedErrorMessage: "",
			expectedType:         SystemResolveConfType,
		},
		{
			name: "Dns spec with upstream typed System with Address fails",
			dns: &DNS{
				TypeMeta: metav1.TypeMeta{
					Kind:       "DNS",
					APIVersion: "operator.openshift.io/v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "default",
				},
				Spec: DNSSpec{
					UpstreamResolvers: UpstreamResolvers{
						Upstreams: []Upstream{
							{
								Type:    SystemResolveConfType,
								Address: "1.2.3.6",
							},
						},
						Policy: RoundRobinForwardingPolicy,
					},
				},
			},
			expectedErrorMessage: "\"spec.upstreamResolvers.upstreams\" must validate at least one schema (anyOf)",
			expectedType:         SystemResolveConfType,
		},
		{
			name: "Dns spec with type upstream Network without Address fails",
			dns: &DNS{
				TypeMeta: metav1.TypeMeta{
					Kind:       "DNS",
					APIVersion: "operator.openshift.io/v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "default",
				},
				Spec: DNSSpec{
					UpstreamResolvers: UpstreamResolvers{
						Upstreams: []Upstream{
							{
								Type: NetworkResolverType,
							},
						},
						Policy: RoundRobinForwardingPolicy,
					},
				},
			},
			expectedErrorMessage: "Unsupported value: \"Network\": supported values: \"\", \"SystemResolvConf\"", //Is it possible to modify crd to have a clearer error message?
			expectedType:         NetworkResolverType,
		},
		{
			name: "Dns spec with network upstream passes",
			dns: &DNS{
				TypeMeta: metav1.TypeMeta{
					Kind:       "DNS",
					APIVersion: "operator.openshift.io/v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "default",
				},
				Spec: DNSSpec{
					UpstreamResolvers: UpstreamResolvers{
						Upstreams: []Upstream{
							{
								Type:    NetworkResolverType,
								Address: "1.2.3.4",
							},
						},
						Policy: RoundRobinForwardingPolicy,
					},
				},
			},
			expectedErrorMessage: "",
			expectedPort:         53,
			expectedAddress:      "1.2.3.4",
			expectedType:         NetworkResolverType,
		},
		{
			name: "Dns spec with network upstream passes",
			dns: &DNS{
				TypeMeta: metav1.TypeMeta{
					Kind:       "DNS",
					APIVersion: "operator.openshift.io/v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "default",
				},
				Spec: DNSSpec{
					UpstreamResolvers: UpstreamResolvers{
						Upstreams: []Upstream{
							{
								Type:    NetworkResolverType,
								Address: "1.2.3.4",
								Port:    5354,
							},
						},
						Policy: RoundRobinForwardingPolicy,
					},
				},
			},
			expectedErrorMessage: "",
			expectedPort:         5354,
			expectedAddress:      "1.2.3.4",
			expectedType:         NetworkResolverType,
		},
		{
			name: "Dns spec with network upstream with wrong Address fails",
			dns: &DNS{
				TypeMeta: metav1.TypeMeta{
					Kind:       "DNS",
					APIVersion: "operator.openshift.io/v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "default",
				},
				Spec: DNSSpec{
					UpstreamResolvers: UpstreamResolvers{
						Upstreams: []Upstream{
							{
								Type:    NetworkResolverType,
								Address: "this is no address",
								Port:    5354,
							},
						},
						Policy: RoundRobinForwardingPolicy,
					},
				},
			},
			expectedErrorMessage: "address in body must be of type ipv4",
		},
		{
			name: "Dns spec with network upstream with wrong Address fails",
			dns: &DNS{
				TypeMeta: metav1.TypeMeta{
					Kind:       "DNS",
					APIVersion: "operator.openshift.io/v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "default",
				},
				Spec: DNSSpec{
					UpstreamResolvers: UpstreamResolvers{
						Upstreams: []Upstream{
							{
								Type:    NetworkResolverType,
								Address: "1.23.4.44",
								Port:    99999,
							},
						},
						Policy: RoundRobinForwardingPolicy,
					},
				},
			},
			expectedErrorMessage: "port in body should be less than or equal to 65535",
		},
		{
			name: "Dns spec with network upstream with wrong IPV6 Address fails",
			dns: &DNS{
				TypeMeta: metav1.TypeMeta{
					Kind:       "DNS",
					APIVersion: "operator.openshift.io/v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "default",
				},
				Spec: DNSSpec{
					UpstreamResolvers: UpstreamResolvers{
						Upstreams: []Upstream{
							{
								Type:    NetworkResolverType,
								Address: "1001::2222:3333::4444",
								Port:    53,
							},
						},
						Policy: RoundRobinForwardingPolicy,
					},
				},
			},
			expectedErrorMessage: "address in body must be of type ipv6",
		},
		{
			name: "Dns spec with upstream typed System with Port fails",
			dns: &DNS{
				TypeMeta: metav1.TypeMeta{
					Kind:       "DNS",
					APIVersion: "operator.openshift.io/v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "default",
				},
				Spec: DNSSpec{
					UpstreamResolvers: UpstreamResolvers{
						Upstreams: []Upstream{
							{
								Type: SystemResolveConfType,
								Port: 53,
							},
						},
						Policy: RoundRobinForwardingPolicy,
					},
				},
			},
			expectedErrorMessage: "\"spec.upstreamresolvers.upstreams\" must validate at least one schema (anyOf)",
			expectedType:         SystemResolveConfType,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			z, err := runtime.DefaultUnstructuredConverter.ToUnstructured(tc.dns)
			if err != nil {
				t.Errorf("toUnstructured unexpected error: %v", err)
			}
			u := unstructured.Unstructured{Object: z}
			_, err = client.Resource(gvr).Create(context.TODO(), &u, metav1.CreateOptions{})
			if tc.expectedErrorMessage != "" && err == nil {
				t.Errorf("Expecting following error but didnt get any:\n%s", tc.expectedErrorMessage)
				t.FailNow()
			}
			if err != nil {
				assert.Containsf(t, err.Error(), tc.expectedErrorMessage, "expected error containing %q, got %s", tc.expectedErrorMessage, err)
			} else {
				v, err := client.Resource(gvr).Get(context.TODO(), "default", metav1.GetOptions{})
				if err != nil {
					t.Error(err.Error())
				}
				var savedDNS DNS
				err = runtime.DefaultUnstructuredConverter.FromUnstructured(v.Object, &savedDNS)
				if err != nil {
					t.Error(err.Error())
				}
				if tc.expectedPort > 0 {
					assert.Equal(t, savedDNS.Spec.UpstreamResolvers.Upstreams[0].Port, tc.expectedPort)
				}
				if tc.expectedAddress != "" {
					assert.Equal(t, savedDNS.Spec.UpstreamResolvers.Upstreams[0].Address, tc.expectedAddress)
				}
				if tc.expectedType != "" {
					assert.Equal(t, savedDNS.Spec.UpstreamResolvers.Upstreams[0].Type, tc.expectedType)
					if tc.expectedType == SystemResolveConfType {
						assert.Emptyf(t, savedDNS.Spec.UpstreamResolvers.Upstreams[0].Address, "Address should be empty")
					}
				}
				err = client.Resource(gvr).Delete(context.TODO(), "default", metav1.DeleteOptions{})
				if err != nil {
					t.Error(err.Error())
				}
			}

		})
	}

}
