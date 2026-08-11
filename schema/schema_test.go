package schema

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/acidsailor/confetti/graph"
	"github.com/acidsailor/confetti/internal/testtypes"
	"github.com/acidsailor/confetti/value"
)

func TestBuilderStructure(t *testing.T) {
	s := New()
	testtypes.Fill(s.Registry)
	iface := s.Node("interface {{ name:ifname }}").Card(ZeroToN)
	iface.Child("ip address {{ ip:ipv4 }} {{ mask:ipv4 }}").
		Card(ZeroToOne).
		MarkIdempotent()
	iface.Child("switchport access vlan {{ vlan:vlan }}").
		Card(ZeroToOne).
		Ref("vlan", "vlan.id")
	s.Node("vlan {{ id:vlan }}").Card(ZeroToN).Kind("vlan").Key("id")

	require.Len(t, s.Roots, 2)
	require.Len(t, iface.Children, 2)
	assert.Equal(t, ZeroToN, iface.Cardinality)
	assert.True(t, iface.Children[0].Idempotent)

	ref := iface.Children[1].Refs
	require.Len(t, ref, 1)
	assert.Equal(
		t,
		Ref{FromArg: "vlan", TargetKind: "vlan", TargetKey: "id"},
		ref[0],
	)

	vlan := s.Roots[1]
	assert.Equal(t, "vlan", vlan.KindName)
	assert.Equal(t, []string{"id"}, vlan.KeyArgs)
}

func TestMatchChildPrefersSpecific(t *testing.T) {
	s := New()
	testtypes.Fill(s.Registry)
	iface := s.Node("interface {{ name:ifname }}")
	plain := iface.Child("ip address {{ ip:ipv4 }} {{ mask:ipv4 }}")
	secondary := iface.Child(
		"ip address {{ ip:ipv4 }} {{ mask:ipv4 }} secondary",
	)

	got, _, ok := MatchChild(
		iface.Children,
		"ip address 10.0.0.1 255.255.255.0 secondary",
	)
	require.True(t, ok)
	assert.Same(t, secondary, got)

	got, _, ok = MatchChild(
		iface.Children,
		"ip address 10.0.0.1 255.255.255.0",
	)
	require.True(t, ok)
	assert.Same(t, plain, got)
}

func TestNodeRenderRoundTrip(t *testing.T) {
	s := New()
	testtypes.Fill(s.Registry)
	n := s.Node("interface {{ name:ifname }}")
	_, f, ok := MatchChild(s.Roots, "interface Ethernet1/1")
	require.True(t, ok)
	assert.Equal(t, "interface Ethernet1/1", n.Render(f))
}

func TestLazyNonTerminalCapture(t *testing.T) {
	reg := value.NewRegistry()
	require.NoError(t, reg.Register(value.Type{Name: "phrase", Pattern: `.+`}))
	// `a` is non-terminal (a trailing " mid {{ b }}" follows) so it must be
	// lazy; `b` is terminal and may stay greedy.
	s, err := compileSpec("pre {{ a:phrase }} mid {{ b:phrase }}", reg)
	require.NoError(t, err)
	f, ok := s.Match("pre x mid y mid z")
	require.True(t, ok)
	assert.Equal(t, "x", f["a"])       // lazy: binds the first " mid "
	assert.Equal(t, "y mid z", f["b"]) // greedy terminal: the rest
	assert.Equal(t, "pre x mid y mid z", s.Render(f))
}

func TestLazifyKeepsEscapedLiteralRequired(t *testing.T) {
	reg := value.NewRegistry()
	// A custom type whose pattern ends in an escaped literal '+'. lazify must
	// not append '?' to it (which would make the '+' optional).
	require.NoError(
		t,
		reg.Register(value.Type{Name: "plusword", Pattern: `\S+\+`}),
	)
	s, err := compileSpec("tag {{ v:plusword }} end", reg) // v is non-terminal
	require.NoError(t, err)

	_, ok := s.Match("tag abc+ end")
	assert.True(t, ok)
	_, ok = s.Match("tag abc end") // missing the required trailing '+'
	assert.False(t, ok, "escaped trailing + must stay required, not optional")
}

func TestSectionExitDeclaration(t *testing.T) {
	s := New()
	testtypes.Fill(s.Registry)
	af := s.Node("address-family {{ afi:word }}").
		SectionExit("exit-address-family")
	assert.Equal(t, "exit-address-family", af.SectionExitToken)
	assert.Equal(t, "", s.Node("vlan {{ id:vlan }}").SectionExitToken)
}

func TestNegateStrategy(t *testing.T) {
	s := New()
	n := s.Node("session-limit {{ n:uint }}").NegateAs("session-limit 32")
	assert.Equal(t, NegTemplate, n.Negate.Kind)
	assert.Equal(t, "session-limit 32", n.Negate.Template)
	assert.Equal(t, NegNoPrefix, s.Node("shutdown").Negate.Kind)
}

func TestBlockBuilders(t *testing.T) {
	s := New()
	b := s.Node("banner motd {{ delim:word }}").BlockDelim("delim")
	assert.Equal(t, BlockDelim, b.Block.Kind)
	assert.Equal(t, "delim", b.Block.Arg)

	u := s.Node("certificate {{ name:word }}").BlockUntil("quit")
	assert.Equal(t, BlockUntil, u.Block.Kind)
	assert.Equal(t, "quit", u.Block.Terminator)

	assert.Equal(t, BlockNone, s.Node("plain").Block.Kind)
}

func TestBlockDelimUnknownArgPanics(t *testing.T) {
	s := New()
	assert.Panics(t, func() {
		s.Node("banner motd {{ delim:word }}").BlockDelim("nope")
	})
}

func TestBlockDelimEmptyMatchingTypePanics(t *testing.T) {
	s := New()
	require.NoError(t, s.Registry.Register(value.Type{
		Name:    "maybe",
		Pattern: `[a-z]*`,
	}))
	assert.Panics(t, func() {
		s.Node("banner motd {{ delim:maybe }}").BlockDelim("delim")
	})
}

func TestBlockNodeCannotHaveChildren(t *testing.T) {
	s := New()
	blk := s.Node("banner motd {{ delim:word }}").BlockDelim("delim")
	assert.Panics(t, func() { blk.Child("x") })
	assert.Panics(t, func() { blk.Adopt(s.Node("y")) })

	sec := s.Node("interface {{ n:word }}")
	sec.Child("mtu {{ m:uint }}")
	assert.Panics(t, func() { sec.BlockUntil("quit") })
}

func TestAdoptRejectsInvalidChildren(t *testing.T) {
	s := New()
	parent := s.Node("parent")
	assert.Panics(t, func() { parent.Adopt(nil) })
	assert.Panics(t, func() { parent.Adopt(New().Node("foreign")) })
	assert.NotPanics(t, func() { parent.Adopt(s.Node("local")) })
}

func TestKeyRejectsUnknownAndDuplicateArgs(t *testing.T) {
	assert.Panics(t, func() { New().Node("x {{ id:word }}").Key("nope") })
	assert.Panics(t, func() { New().Node("x {{ id:word }}").Key("id", "id") })
}

func TestUniqueUnknownArgPanics(t *testing.T) {
	s := New()
	assert.Panics(t, func() {
		s.Node("router {{ proto:word }}").Unique("protoz")
	})
}

func TestOrderingMetadata(t *testing.T) {
	s := New()
	n := s.Node("router {{ proto:word }}").
		Requires("feature").
		Unique("proto")
	assert.Equal(t, []string{"feature"}, n.RequiresKinds)
	assert.Equal(t, []string{"proto"}, n.UniqueArgs)

	called := 0
	s.OrderHook(func(*graph.Graph) { called++ })
	s.OrderHook(func(*graph.Graph) { called++ })
	require.Len(t, s.OrderHooks, 2)
	for _, h := range s.OrderHooks {
		h(nil)
	}
	assert.Equal(t, 2, called)
}

func TestTogglesLinksBothSides(t *testing.T) {
	s := New()
	testtypes.Fill(s.Registry)
	iface := s.Node("interface {{ name:ifname }}").Card(ZeroToN)
	shut := iface.Child("shutdown").Card(ZeroToOne)
	noshut := iface.Child("no shutdown").Card(ZeroToOne).Toggles(shut)
	assert.Same(t, shut, noshut.TogglePartner())
	assert.Same(t, noshut, shut.TogglePartner())
	// Canonical member = the Toggles argument, for both sides.
	assert.Same(t, shut, noshut.ToggleCanonical())
	assert.Same(t, shut, shut.ToggleCanonical())
}

func TestToggleUnpaired(t *testing.T) {
	s := New()
	n := s.Node("shutdown").Card(ZeroToOne)
	assert.Nil(t, n.TogglePartner())
	assert.Same(t, n, n.ToggleCanonical())
}

func TestRefRejectsMalformedTarget(t *testing.T) {
	for _, target := range []string{"vrf", "vrf.", ".name", ""} {
		s := New()
		n := s.Node("x {{ v:word }}").Card(ZeroToOne)
		assert.Panics(t, func() { n.Ref("v", target) }, target)
	}
}

func TestToggleGroupThreeWay(t *testing.T) {
	s := New()
	a := s.Node("duplex auto").Card(ZeroToOne)
	f := s.Node("duplex full").Card(ZeroToOne)
	h := s.Node("duplex half").Card(ZeroToOne).Toggles(a, f)
	for _, m := range []*Node{a, f, h} {
		assert.Len(t, m.ToggleGroup, 3)
		// Each member uses the first Toggles argument as canonical.
		assert.Same(t, a, m.ToggleCanonical())
		// No single partner in an N-way group.
		assert.Nil(t, m.TogglePartner())
	}
}

func TestToggleGroupRejectsRegrouping(t *testing.T) {
	s := New()
	a := s.Node("duplex auto").Card(ZeroToOne)
	s.Node("duplex full").Card(ZeroToOne).Toggles(a)
	assert.Panics(t, func() {
		s.Node("duplex half").Card(ZeroToOne).Toggles(a)
	})
}

func TestToggleGroupRejectsDuplicateMember(t *testing.T) {
	s := New()
	a := s.Node("duplex auto").Card(ZeroToOne)
	assert.Panics(t, func() {
		s.Node("duplex full").Card(ZeroToOne).Toggles(a, a)
	})
}

func TestTogglesPanics(t *testing.T) {
	assert.Panics(t, func() { // nil partner
		New().Node("shutdown").Card(ZeroToOne).Toggles(nil)
	})
	assert.Panics(t, func() { // self-pairing
		n := New().Node("shutdown").Card(ZeroToOne)
		n.Toggles(n)
	})
	assert.Panics(t, func() { // keyed partner
		s := New()
		testtypes.Fill(s.Registry)
		k := s.Node("vlan {{ id:vlan }}").Card(ZeroToN).Key("id")
		s.Node("no shutdown").Card(ZeroToOne).Toggles(k)
	})
	assert.Panics(t, func() { // wrong cardinality receiver
		s := New()
		p := s.Node("shutdown").Card(ZeroToOne)
		s.Node("no shutdown").Card(ZeroToN).Toggles(p)
	})
	assert.Panics(t, func() { // re-pairing an already-paired node
		s := New()
		a := s.Node("shutdown").Card(ZeroToOne)
		b := s.Node("no shutdown").Card(ZeroToOne)
		b.Toggles(a)
		s.Node("shutdown v2").Card(ZeroToOne).Toggles(a)
	})
}

func TestNegationWordDefaultsToNo(t *testing.T) {
	s := New()
	n := s.Node("shutdown").Card(ZeroToOne)
	assert.Equal(t, "no", s.NegationWord)
	assert.Equal(t, "no", n.Schema.NegationWord)
}

func TestNegationWordOverride(t *testing.T) {
	s := New().Negation("undo")
	n := s.Node("shutdown").Card(ZeroToOne)
	assert.Equal(t, "undo", s.NegationWord)
	assert.Equal(t, "undo", n.Schema.NegationWord)
}

func TestNegationEmptyWordPanics(t *testing.T) {
	assert.Panics(t, func() { New().Negation("") })
}

func TestProtected(t *testing.T) {
	s := New()
	testtypes.Fill(s.Registry)
	plain := s.Node("shutdown").Card(ZeroToOne)
	prot := s.Node("router bgp {{ asn:asn }}").Card(ZeroToOne).Protect()
	assert.False(t, plain.Protected)
	assert.True(t, prot.Protected)
}

func TestListDeclaration(t *testing.T) {
	s := New()
	testtypes.Fill(s.Registry)
	n := s.Node("allowed vlan {{ vlans:word }}").
		List("vlans", "uint").
		ListDelta("allowed vlan add {{ vlans }}", "allowed vlan remove {{ vlans }}")
	assert.Equal(t, ListStrategy{
		Arg:        "vlans",
		Elem:       "uint",
		AddTmpl:    "allowed vlan add {{ vlans }}",
		RemoveTmpl: "allowed vlan remove {{ vlans }}",
	}, n.ListSpec)
	// A set-valued slot is idempotent by nature; List declares it once.
	assert.True(t, n.Idempotent)
	// Zero value on ordinary nodes.
	assert.Equal(t, ListStrategy{}, s.Node("plain").ListSpec)
}

func TestListPanics(t *testing.T) {
	tests := []struct {
		name string
		fn   func(s *Schema)
	}{
		{"unknown arg", func(s *Schema) {
			s.Node("allowed vlan {{ vlans:word }}").List("nope", "uint")
		}},
		{"unregistered elem type", func(s *Schema) {
			s.Node("allowed vlan {{ vlans:word }}").List("vlans", "nope")
		}},
		{"list arg is key arg (Key first)", func(s *Schema) {
			s.Node("g {{ vlans:word }}").Key("vlans").List("vlans", "uint")
		}},
		{"list arg is key arg (List first)", func(s *Schema) {
			s.Node("g {{ vlans:word }}").List("vlans", "uint").Key("vlans")
		}},
		{"ListDelta without List", func(s *Schema) {
			s.Node("allowed vlan {{ vlans:word }}").
				ListDelta("a {{ vlans }}", "r {{ vlans }}")
		}},
		{"ListDelta add template missing list arg", func(s *Schema) {
			s.Node("allowed vlan {{ vlans:word }}").List("vlans", "uint").
				ListDelta("add all", "r {{ vlans }}")
		}},
		{"ListDelta remove template missing list arg", func(s *Schema) {
			s.Node("allowed vlan {{ vlans:word }}").List("vlans", "uint").
				ListDelta("a {{ vlans }}", "remove all")
		}},
		{"list node cannot have children (Child first)", func(s *Schema) {
			n := s.Node("g {{ v:word }}")
			n.Child("x")
			n.List("v", "uint")
		}},
		{"list node cannot have children (List first)", func(s *Schema) {
			n := s.Node("g {{ v:word }}").List("v", "uint")
			n.Child("x")
		}},
		{"list node cannot adopt children", func(s *Schema) {
			n := s.Node("g {{ v:word }}").List("v", "uint")
			child := s.Node("x")
			n.Adopt(child)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Panics(t, func() { tt.fn(New()) })
		})
	}
}

func TestMembersDeclaration(t *testing.T) {
	s := New()
	n := s.Node("vlan {{ ids:word }}").List("ids", "uint").Members("vlan")
	assert.Equal(t, "vlan", n.MembersKind)
	// Zero value on ordinary and plain-list nodes.
	assert.Equal(t, "", s.Node("plain").MembersKind)
	assert.Equal(t, "",
		s.Node("g {{ v:word }}").List("v", "uint").MembersKind)
}

func TestMembersPanics(t *testing.T) {
	tests := []struct {
		name string
		fn   func(s *Schema)
	}{
		{"empty kind", func(s *Schema) {
			s.Node("vlan {{ ids:word }}").List("ids", "uint").Members("")
		}},
		{"Members without List", func(s *Schema) {
			s.Node("vlan {{ ids:word }}").Members("vlan")
		}},
		{"Members after ListDelta", func(s *Schema) {
			s.Node("vlan {{ ids:word }}").List("ids", "uint").
				ListDelta("a {{ ids }}", "r {{ ids }}").Members("vlan")
		}},
		{"ListDelta after Members", func(s *Schema) {
			s.Node("vlan {{ ids:word }}").List("ids", "uint").
				Members("vlan").ListDelta("a {{ ids }}", "r {{ ids }}")
		}},
		{"Members after Key", func(s *Schema) {
			s.Node("vlan {{ ids:word }} {{ k:word }}").Key("k").
				List("ids", "uint").Members("vlan")
		}},
		{"Key after Members", func(s *Schema) {
			s.Node("vlan {{ ids:word }} {{ k:word }}").List("ids", "uint").
				Members("vlan").Key("k")
		}},
		{"Members after block", func(s *Schema) {
			s.Node("vlan {{ ids:word }}").List("ids", "uint").
				BlockUntil("end").Members("vlan")
		}},
		{"block after Members", func(s *Schema) {
			s.Node("vlan {{ ids:word }}").List("ids", "uint").
				Members("vlan").BlockUntil("end")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Panics(t, func() { tt.fn(New()) })
		})
	}
}

func TestListSepAndKeywordsDeclaration(t *testing.T) {
	s := New()
	n := s.Node("ports {{ p:rest }}").List("p", "uint").
		ListSep(" ").
		ListKeywords("none", "all", "except", "1-8")
	ls := n.ListSpec
	assert.Equal(t, " ", ls.Sep)
	assert.Equal(t, "none", ls.NoneWord)
	assert.Equal(t, "all", ls.AllWord)
	assert.Equal(t, "except", ls.ExceptWord)
	assert.Equal(t, "1-8", ls.Domain)
	// The adapter mirrors the fields one-to-one.
	kw := ls.Keywords()
	assert.Equal(t, "none", kw.None)
	assert.Equal(t, "1-8", kw.Domain)
}

func TestListContinuesDeclaration(t *testing.T) {
	s := New()
	base := s.Node("vlan {{ v:word }}").List("v", "uint")
	cont := s.Node("vlan add {{ v:word }}").List("v", "uint").
		ListContinues(base)
	assert.Same(t, base, cont.ListContinuation)
	assert.Nil(t, base.ListContinuation)
}

func TestListSeamsPanics(t *testing.T) {
	base := func(s *Schema) *Node {
		return s.Node("vlan {{ v:word }}").List("v", "uint")
	}
	tests := []struct {
		name string
		fn   func(s *Schema)
	}{
		{"ListSep without List", func(s *Schema) {
			s.Node("g {{ v:word }}").ListSep(" ")
		}},
		{"ListSep empty separator", func(s *Schema) {
			base(s).ListSep("")
		}},
		{"ListKeywords without List", func(s *Schema) {
			s.Node("g {{ v:word }}").ListKeywords("none", "", "", "")
		}},
		{"ListKeywords all without domain", func(s *Schema) {
			base(s).ListKeywords("", "all", "", "")
		}},
		{"ListKeywords except without domain", func(s *Schema) {
			base(s).ListKeywords("", "", "except", "")
		}},
		{"ListKeywords domain does not expand", func(s *Schema) {
			base(s).ListKeywords("", "all", "", "9-5")
		}},
		{"domain invalid under later ListSep", func(s *Schema) {
			// Changing the separator exposes the reversed range and must revalidate the domain.
			base(s).ListKeywords("", "all", "", "1 9-5").ListSep(" ")
		}},
		{"ListContinues without List", func(s *Schema) {
			b := base(s)
			s.Node("vlan add {{ v:word }}").ListContinues(b)
		}},
		{"ListContinues nil base", func(s *Schema) {
			base(s).ListContinues(nil)
		}},
		// ListContinues(self) is a valid self-union slot and can coexist with ListDelta.
		{"ListContinues non-list base", func(s *Schema) {
			b := s.Node("plain")
			base(s).ListContinues(b)
		}},
		{"ListContinues chained base", func(s *Schema) {
			b := base(s)
			c := s.Node("vlan add {{ v:word }}").List("v", "uint").
				ListContinues(b)
			s.Node("vlan also {{ v:word }}").List("v", "uint").
				ListContinues(c)
		}},
		{"base Key after ListContinues", func(s *Schema) {
			// Reject a keyed base in both method-call orders.
			b := s.Node("acl {{ k:word }} {{ v:word }}").List("v", "uint")
			s.Node("acl add {{ v:word }}").List("v", "uint").ListContinues(b)
			b.Key("k")
		}},
		{"ListContinues keyed base", func(s *Schema) {
			b := s.Node("acl {{ k:word }} {{ v:word }}").Key("k").
				List("v", "uint")
			base(s).ListContinues(b)
		}},
		{"ListContinues after ListDelta", func(s *Schema) {
			b := base(s)
			s.Node("vlan add {{ v:word }}").List("v", "uint").
				ListDelta("a {{ v }}", "r {{ v }}").ListContinues(b)
		}},
		{"ListDelta after ListContinues", func(s *Schema) {
			b := base(s)
			s.Node("vlan add {{ v:word }}").List("v", "uint").
				ListContinues(b).ListDelta("a {{ v }}", "r {{ v }}")
		}},
		{"ListContinues after Members", func(s *Schema) {
			b := base(s)
			s.Node("vlan add {{ v:word }}").List("v", "uint").
				Members("vlan").ListContinues(b)
		}},
		{"Members after ListContinues", func(s *Schema) {
			b := base(s)
			s.Node("vlan add {{ v:word }}").List("v", "uint").
				ListContinues(b).Members("vlan")
		}},
		{"Key after ListContinues", func(s *Schema) {
			b := base(s)
			s.Node("vlan add {{ v:word }} {{ k:word }}").List("v", "uint").
				ListContinues(b).Key("k")
		}},
		{"ListContinues after Key", func(s *Schema) {
			b := base(s)
			s.Node("vlan add {{ v:word }} {{ k:word }}").Key("k").
				List("v", "uint").ListContinues(b)
		}},
		{"ListContinues after block", func(s *Schema) {
			b := base(s)
			s.Node("vlan add {{ v:word }}").List("v", "uint").
				BlockUntil("end").ListContinues(b)
		}},
		{"block after ListContinues", func(s *Schema) {
			b := base(s)
			s.Node("vlan add {{ v:word }}").List("v", "uint").
				ListContinues(b).BlockUntil("end")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Panics(t, func() { tt.fn(New()) })
		})
	}
}

func TestEmptyOnRemoveDeclaration(t *testing.T) {
	s := New()
	n := s.Node("vlan database").ClearOnRemove()
	assert.True(t, n.EmptyOnRemove)
	assert.False(t, s.Node("plain").EmptyOnRemove)
}

func TestNegateFuncSetsStrategy(t *testing.T) {
	s := New()
	n := s.Node("x {{ id:uint }}").
		NegateFunc(func(f map[string]string, rendered string) string {
			return "clear x " + f["id"]
		})
	ns := n.Negate
	assert.Equal(t, NegFunc, ns.Kind)
	require.NotNil(t, ns.Func)
	assert.Equal(t,
		"clear x 7",
		ns.Func(map[string]string{"id": "7"}, "x 7"))
}

func TestNegateFuncNilPanics(t *testing.T) {
	s := New()
	assert.PanicsWithValue(t,
		"schema: NegateFunc with nil func: x",
		func() { s.Node("x").NegateFunc(nil) })
}

func TestEmptyOnRemovePanics(t *testing.T) {
	tests := []struct {
		name string
		fn   func(s *Schema)
	}{
		{"NegateAs first", func(s *Schema) {
			s.Node("x {{ id:uint }}").NegateAs("no x {{ id }}").ClearOnRemove()
		}},
		{"NegateAs second", func(s *Schema) {
			s.Node("x {{ id:uint }}").ClearOnRemove().NegateAs("no x {{ id }}")
		}},
		{"NegateDefault first", func(s *Schema) {
			s.Node("x").NegateDefault().ClearOnRemove()
		}},
		{"NegateDefault second", func(s *Schema) {
			s.Node("x").ClearOnRemove().NegateDefault()
		}},
		{"NegateFunc first", func(s *Schema) {
			s.Node("x").
				NegateFunc(func(map[string]string, string) string {
					return ""
				}).
				ClearOnRemove()
		}},
		{"NegateFunc second", func(s *Schema) {
			s.Node("x").ClearOnRemove().
				NegateFunc(func(map[string]string, string) string {
					return ""
				})
		}},
		{"block first", func(s *Schema) {
			s.Node("banner {{ delim:word }}").
				BlockDelim("delim").ClearOnRemove()
		}},
		{"block second", func(s *Schema) {
			s.Node("banner {{ delim:word }}").
				ClearOnRemove().BlockDelim("delim")
		}},
		{"list first", func(s *Schema) {
			s.Node("g {{ v:word }}").List("v", "uint").ClearOnRemove()
		}},
		{"list second", func(s *Schema) {
			s.Node("g {{ v:word }}").ClearOnRemove().List("v", "uint")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Panics(t, func() { tt.fn(New()) })
		})
	}
}

func TestEmptyOnRemoveProtectedPanics(t *testing.T) {
	// "Never delete this" contradicts "here is how to delete this"; the
	// expansion would silently bypass the header's Protected rail.
	assert.Panics(t, func() {
		New().Node("vlan database").Protect().ClearOnRemove()
	})
	assert.Panics(t, func() {
		New().Node("vlan database").ClearOnRemove().Protect()
	})
}

func TestRespellAsRails(t *testing.T) {
	mk := func() (*Schema, *Node) {
		s := New()
		n := s.Node("acl {{ id:uint }} {{ act:word }}").Card(ZeroToN)
		return s, n
	}
	// header must reference a capture arg
	_, n := mk()
	assert.Panics(t, func() { n.RespellAs("ip acl standard") })
	_, n = mk()
	assert.Panics(t, func() { n.RespellAs("ip acl {{ id }} {{ typo }}") })
	_, n = mk()
	assert.Panics(t, func() { n.RespellAs("ip acl {{ id }}", "{{ typo }}") })
	// double declaration
	_, n = mk()
	n.RespellAs("ip acl {{ id }}", "{{ act }}")
	assert.Panics(t, func() { n.RespellAs("ip acl {{ id }}") })
	// Reject children and RespellAs in either method-call order.
	_, n = mk()
	n.RespellAs("ip acl {{ id }}", "{{ act }}")
	assert.Panics(t, func() { n.Child("x") })
	assert.Panics(t, func() { n.Adopt(mkNode(t)) })
	s, n := mk()
	withKid := s.Node("y {{ v:word }}").Card(ZeroToN)
	withKid.Child("z")
	assert.Panics(t, func() { withKid.RespellAs("q {{ v }}") })
	// mutually exclusive with Members / ListDelta / ListContinues / blocks
	s, _ = mk()
	lst := s.Node("m {{ ids:word }}").Card(ZeroToN).List("ids", "uint")
	lst.RespellAs("m2 {{ ids }}")
	assert.Panics(t, func() { lst.Members("k") })
	assert.Panics(t, func() { lst.ListDelta("a {{ ids }}", "r {{ ids }}") })
	s, _ = mk()
	base := s.Node("b {{ ids:word }}").Card(ZeroToOne).List("ids", "uint")
	cont := s.Node("c {{ ids:word }}").Card(ZeroToN).List("ids", "uint").
		RespellAs("c2 {{ ids }}")
	assert.Panics(t, func() { cont.ListContinues(base) })
	_, n = mk()
	n.RespellAs("ip acl {{ id }}")
	assert.Panics(t, func() { n.BlockUntil("end") })
	// reverse orders
	s, _ = mk()
	mem := s.Node("mm {{ ids:word }}").Card(ZeroToN).
		List("ids", "uint").Members("k")
	assert.Panics(t, func() { mem.RespellAs("mm2 {{ ids }}") })
	s, _ = mk()
	blk := s.Node("banner {{ d:word }}").Card(ZeroToOne).BlockDelim("d")
	assert.Panics(t, func() { blk.RespellAs("b2 {{ d }}") })
}

func mkNode(t *testing.T) *Node {
	t.Helper()
	return New().Node("orphan {{ v:word }}").Card(ZeroToN)
}

func TestNegateAsUnknownArgPanics(t *testing.T) {
	s := New()
	// A typo'd placeholder would interpolate as "" and emit corrupt CLI.
	assert.Panics(t, func() {
		s.Node("router bgp {{ asn:uint }}").NegateAs("no router bgp {{ as }}")
	})
	// Known args and literal-only templates stay legal.
	assert.NotPanics(t, func() {
		s.Node("router ospf {{ id:uint }}").NegateAs("no router ospf {{ id }}")
		s.Node("banner motd").NegateAs("no banner motd")
	})
}

func TestListDeltaUnknownArgPanics(t *testing.T) {
	s := New()
	assert.Panics(t, func() {
		s.Node("allowed vlan {{ vlans:word }}").List("vlans", "uint").
			ListDelta("add {{ vlans }} {{ nope }}", "remove {{ vlans }}")
	})
	assert.Panics(t, func() {
		s.Node("allowed vlan {{ vlans:word }}").List("vlans", "uint").
			ListDelta("add {{ vlans }}", "remove {{ vlans }} {{ nope }}")
	})
}

func TestRefUnknownFromArgPanics(t *testing.T) {
	s := New()
	assert.Panics(t, func() {
		s.Node("x {{ v:word }}").Ref("nope", "vlan.id")
	})
}

func TestRequiresEmptyKindPanics(t *testing.T) {
	assert.Panics(t, func() { New().Node("x").Requires("") })
}

func TestToggleMemberRejectsKeyAndCard(t *testing.T) {
	// The Toggles rails (non-keyed ZeroToOne) must hold AFTER grouping too.
	s := New()
	a := s.Node("shutdown").Card(ZeroToOne)
	b := s.Node("no shutdown").Card(ZeroToOne).Toggles(a)
	assert.Panics(t, func() { a.Key("x") })
	assert.Panics(t, func() { b.Card(ZeroToN) })
}

func TestBlockUntilEmptyPanics(t *testing.T) {
	assert.Panics(t, func() { New().Node("banner motd").BlockUntil("") })
}

func TestMatchChildRecomputesAfterNewChild(t *testing.T) {
	// The specificity-order memo must not go stale when the child slice grows.
	s := New()
	testtypes.Fill(s.Registry)
	iface := s.Node("interface {{ name:ifname }}")
	plain := iface.Child("ip address {{ ip:ipv4 }} {{ mask:ipv4 }}")
	line := "ip address 10.0.0.1 255.255.255.0 secondary"

	_, _, ok := MatchChild(iface.Children, line)
	require.False(t, ok) // primes the memo with only the plain form

	secondary := iface.Child(
		"ip address {{ ip:ipv4 }} {{ mask:ipv4 }} secondary",
	)
	got, _, ok := MatchChild(iface.Children, line)
	require.True(t, ok)
	assert.Same(t, secondary, got)
	got, _, ok = MatchChild(
		iface.Children,
		"ip address 10.0.0.1 255.255.255.0",
	)
	require.True(t, ok)
	assert.Same(t, plain, got)
}

func TestInterpolate(t *testing.T) {
	fields := map[string]string{"a": "1", "b": "2"}
	tests := []struct{ tmpl, want string }{
		{"no x {{ a }} y {{ b }}", "no x 1 y 2"},
		{"literal only", "literal only"},
		{"unknown {{ nope }}!", "unknown !"},
		{"dangling {{ a", "dangling {{ a"}, // unterminated opener: verbatim
		{"a}}b {{ a }}", "a}}b 1"},         // stray earlier "}}" stays literal
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, Interpolate(tt.tmpl, fields), tt.tmpl)
	}
}

func TestTemplateRefs(t *testing.T) {
	assert.Equal(t, []string{"a", "b"}, templateRefs("x {{ a }} y {{ b }}"))
	assert.Nil(t, templateRefs("no placeholders"))
	// Unterminated opener: names up to it, dangling tail ignored.
	assert.Equal(t, []string{"a"}, templateRefs("x {{ a }} y {{ b"))
	assert.Nil(t, templateRefs("{{ a"))
}

func TestEmptyCaptureNamePanics(t *testing.T) {
	assert.Panics(t, func() { New().Node("x {{ }}") })
	assert.Panics(t, func() { New().Node("x {{ :word }}") })
}

func TestListContinuesSelfUnion(t *testing.T) {
	// Self-continuation retains the slot, can keep ListDelta, and must work in both method-call orders.
	s := New()
	n := s.Node("vlan add {{ v:word }}").Card(ZeroToOne).List("v", "uint").
		ListDelta("vlan add {{ v }}", "vlan remove {{ v }}")
	n.ListContinues(n)
	assert.Same(t, n, n.ListContinuation)

	s2 := New()
	m := s2.Node("vlan add {{ v:word }}").Card(ZeroToOne).List("v", "uint")
	m.ListContinues(m)
	m.ListDelta("vlan add {{ v }}", "vlan remove {{ v }}")
	assert.Same(t, m, m.ListContinuation)

	// A TRUE (two-def) continuation still excludes ListDelta, either order.
	s3 := New()
	b := s3.Node("vlan {{ v:word }}").Card(ZeroToOne).List("v", "uint")
	c := s3.Node("vlan add {{ v:word }}").Card(ZeroToN).List("v", "uint").
		ListContinues(b)
	assert.Panics(t, func() {
		c.ListDelta("vlan add {{ v }}", "vlan remove {{ v }}")
	})
}
