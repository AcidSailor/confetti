package alpha

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	confetti "github.com/acidsailor/confetti"
	"github.com/acidsailor/confetti/parse"
)

func TestAlphaParsesBGPFixture(t *testing.T) {
	e := Engine(confetti.WithUnknown(parse.Drop))
	in := "" +
		"vlan 10\n" +
		"  name USERS\n" +
		"vrf context RED\n" +
		"route-map TOR_IMPORT permit 10\n" +
		"interface Ethernet1/1\n" +
		"  description UPLINK\n" +
		"  switchport mode access\n" +
		"  switchport access vlan 10\n" +
		"  vrf member RED\n" +
		"  no shutdown\n" +
		"feature bgp\n" +
		"router bgp 65111\n" +
		"  neighbor 10.1.1.2 remote-as 65200\n" +
		"  address-family ipv4 unicast\n" +
		"    neighbor 10.1.1.2 route-map TOR_IMPORT in\n" +
		"    neighbor 10.1.1.2 activate\n"
	cfg, d := e.Import(in)
	require.False(t, d.HasErrors(), d.String())
	cc := e.CommitCheck(cfg)
	require.False(t, cc.HasErrors(), cc.String())
}

func TestAlphaCommitCheckErrors(t *testing.T) {
	e := Engine(confetti.WithUnknown(parse.Drop))
	for _, tc := range []struct{ name, in, want string }{
		{
			name: "dangling route-map",
			in: "router bgp 65111\n" +
				"  address-family ipv4 unicast\n" +
				"    neighbor 10.1.1.2 route-map MISSING in\n",
			want: `route-map "MISSING" does not exist`,
		},
		{
			name: "missing feature bgp",
			in:   "router bgp 65111\n",
			want: "requires a feature-bgp instance",
		},
		{
			// Only the literal "feature bgp" def carries the feature-bgp Kind, so a generic feature must not satisfy it.
			name: "generic feature does not satisfy bgp",
			in:   "feature interface-vlan\nrouter bgp 65111\n",
			want: "requires a feature-bgp instance",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, d := e.Import(tc.in)
			require.False(t, d.HasErrors(), d.String())
			cc := e.CommitCheck(cfg)
			assert.True(t, cc.HasErrors())
			assert.Contains(t, cc.String(), tc.want)
		})
	}
}

func TestAlphaRoundTrip(t *testing.T) {
	e := Engine()
	// An empty want means the input renders back unchanged.
	for _, tc := range []struct{ name, in, want string }{
		{
			name: "route-map children",
			in: "route-map FOO permit 10\n" +
				"  match tag 100\n" +
				"  match ip address ACL1\n" +
				"  set local-preference 200\n" +
				"  set metric 50\n",
		},
		{
			name: "header noise stripped",
			in: "!Command: show running-config\n" +
				"version 9.3\n" +
				"vlan 10\n" +
				"  name X\n",
			want: "vlan 10\n  name X\n",
		},
		{
			// Distinct stanzas under one name are not a duplicate key.
			name: "route-map multiple sequences",
			in:   "route-map FOO permit 10\nroute-map FOO permit 20\n",
		},
		{
			// exit-address-family is re-emitted on render.
			name: "address-family",
			in: "router bgp 65111\n" +
				"  address-family ipv4 unicast\n" +
				"    neighbor 10.1.1.2 activate\n" +
				"  exit-address-family\n",
		},
		{
			name: "banner",
			in:   "banner motd ^\nAuthorized access only\n\n  -- NOC --\n^\nvlan 10\n",
		},
		{
			// Preserve banner body lines because text transforms exclude block spans.
			name: "banner body noise survives",
			in:   "banner motd ^\n!!! Authorized access only !!!\nexit\nversion 9.3\n^\nvlan 10\n",
		},
		{
			// Protect a "!" terminator from the comment-drop rule so later configuration remains outside the block.
			name: "banner bang delimiter terminates",
			in:   "banner motd !\nhello\n!\nvlan 10\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, d := e.Import(tc.in)
			require.False(t, d.HasErrors(), d.String())
			out, _ := e.Render(cfg)
			want := tc.want
			if want == "" {
				want = tc.in
			}
			assert.Equal(t, want, out)
		})
	}
}

func TestAlphaAddressFamilyOptionalSAFI(t *testing.T) {
	e := Engine()
	in := "router bgp 65111\n" +
		"  address-family ipv4\n" +
		"    neighbor 10.1.1.2 activate\n"
	cfg, d := e.Import(in)
	// Preserve children under the accepted SAFI-less form.
	require.False(t, d.HasErrors(), d.String())
	out, _ := e.Render(cfg)
	assert.Contains(t, out, "  address-family ipv4\n")
	assert.Contains(t, out, "    neighbor 10.1.1.2 activate\n")
}

func TestAlphaImportRejectsBadValues(t *testing.T) {
	e := Engine()

	_, d := e.Import("vlan 9999\n")
	assert.True(t, d.HasErrors())
	assert.Contains(t, d.String(), "out of range")

	_, d2 := e.Import(
		"interface Ethernet1/1\n  ip address 10.0.0.300 255.255.255.0\n",
	)
	assert.True(t, d2.HasErrors())
}

func TestDomainTypeChecks(t *testing.T) {
	reg := Schema().Registry
	for _, tc := range []struct {
		name      string
		good, bad []string
	}{
		{
			name: "ipv4",
			good: []string{"10.0.0.1", "255.255.255.0", "10.0.0.1/24"},
			bad:  []string{"10.0.0.1/33", "10.0.0.256", "nope", "2001:db8::1"},
		},
		{
			name: "vlan",
			good: []string{"1", "4094"},
			bad:  []string{"0", "4095", "x"},
		},
		{
			name: "asn",
			good: []string{"65000"},
			bad:  []string{"0", "65536"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			typ, ok := reg.Get(tc.name)
			require.True(t, ok)
			require.NotNil(t, typ.Check)
			for _, s := range tc.good {
				assert.NoError(t, typ.Check(s), s)
			}
			for _, s := range tc.bad {
				assert.Error(t, typ.Check(s), s)
			}
		})
	}
}

func TestAlphaVlanMembershipFoldRoundTrip(t *testing.T) {
	// Fold compressed membership and property-bearing sections into canonical section output.
	e := Engine()
	in := "vlan 1,7-9,411\n" +
		"vlan 411\n" +
		"  name PAYMENTS\n"
	cfg, d := e.Import(in)
	require.False(t, d.HasErrors(), d.String())
	out, _ := e.Render(cfg)
	want := "vlan 1\n" +
		"vlan 7\n" +
		"vlan 8\n" +
		"vlan 9\n" +
		"vlan 411\n" +
		"  name PAYMENTS\n"
	assert.Equal(t, want, out)

	// Canonicalization is idempotent: re-importing the canonical form is a
	// fixed point.
	cfg2, d2 := e.Import(out)
	require.False(t, d2.HasErrors(), d2.String())
	out2, _ := e.Render(cfg2)
	assert.Equal(t, want, out2)
}

func TestAlphaMembershipVlanSatisfiesRef(t *testing.T) {
	// A vlan declared only through the membership spelling is a real
	// canonical instance: refs resolve against it.
	e := Engine(confetti.WithUnknown(parse.Drop))
	cfg, d := e.Import("vlan 5-10\n" +
		"interface Ethernet1/1\n  switchport access vlan 7\n")
	require.False(t, d.HasErrors(), d.String())
	cc := e.CommitCheck(cfg)
	assert.False(t, cc.HasErrors(), cc.String())
}

func TestAlphaMembershipBadItemRejected(t *testing.T) {
	// The fold accepts the pattern, then ImportCheck rejects the synthesized out-of-range VLAN.
	e := Engine()
	_, d := e.Import("vlan 5,5000\n")
	assert.True(t, d.HasErrors())
	assert.Contains(t, d.String(), "5000")
}
