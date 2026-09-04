# Fixture lineage and grammar assumptions

The two schemas in `internal/fixture/` describe imaginary platforms. Each
schema derives from part of a real vendor grammar. This document records that
origin and identifies unverified behavior.
Read it before you compare a fixture with a real device or treat fixture
behavior as vendor-accurate.

| Fixture | Lineage | Grounding |
|---|---|---|
| `internal/fixture/alpha` | Cisco NX-OS running-config slice | Real reference configs |
| `internal/fixture/beta` | IPInfusion OcNOS 6.x/7.0 (cmlsh) | Public documentation only; no running configuration was available |

The schemas remain separate for three reasons. Alpha protects `router bgp`,
but beta removes it in golden tests. Alpha uses a 16-bit ASN, while beta uses
a 4-byte ASN with asdot notation. Strict unknown-input tests also require a
foreign grammar.

## alpha (NX-OS lineage)

Vendor-grounded constructs and why they exist:

- Three references come from real configurations: `switchport access vlan`
  to `vlan`, `vrf member` to `vrf context`, and `neighbor … route-map` to
  `route-map`.
- `address-family {{afi}}` SAFI-less and `{{afi}} {{safi}}` two-token forms
  coexist (sharing children via `Adopt`) because real output emits
  `address-family ipv4` with no SAFI token.
- `banner motd {{delim}}` provides the multi-line block-capture fixture.
  Beta documentation describes only single-line banners.
- `interface {{name:ethport}}` (physical, `NegateDefault` reset form) vs
  `{{name:ifname}}` (logical, plain negation): physical ports reset rather
  than delete. The templates have equal literal specificity, so the physical
  definition must appear first.
- `vlan {{ids}}` membership line + per-vlan sections: real NX-OS emits both
  spellings simultaneously, with the membership line first. This requires a
  complete level scan before folding.
- Trunk allowed-VLAN grammar includes the documented keyword
  spellings (`none`/`all`/`except`), `add` continuation lines, and
  add/remove delta forms.
- Import drop rules (`!` comments, `version`, `exit`, config preamble)
  mirror real `show running-config` noise.
- Reference configurations print neither `vlan 1` nor `vrf context default`,
  so `Baseline()` declares both.

Unverified / docs-only (re-verify on a lab device if it ever matters):

- `default interface <name>` reset form is documented but was not verified on
  a real image.
- Default administrative state is not modeled because it depends on the
  platform and port.
- `asn` deliberately 16-bit here (4-byte lives in beta).
- route-map inner `match`/`set` children were added without a cited vendor
  source (header + ref target were the grounded part).
- The `feature bgp` → `router bgp` dependency is confetti-specific data, not
  from references.
- Reserved-vlan sub-range (3968–4095) not modeled; keyword domain is flat
  `1-4094`.
- `Baseline()` models VLAN 1 and the default VRF as device-provided, and
  `vrf member default` as their referrer. The device behavior was not
  confirmed on a lab image.
- Single-line banner form (`banner motd ^text^` on one line) out of scope.

## beta (OcNOS lineage)

All beta behavior comes from docs.ipinfusion.com. Verify the complete grammar
against a device or a containerlab `ipinfusion_ocnos` image. The following
constructs have documentation support:

- `bridge` / `vlan database` model: VLANs are nested under a `vlan
  database` mode, keyed `(id, bridge)` with mutable `state`. This tests a
  keyed-leaf Modify. `vlan database` is a mode, not an object: `no vlan
  database` is not a real command, which is why `EmptyOnRemove` exists (its
  only use in the repository).
- Named and unnamed VLAN templates share Kind+Key, so a rename pairs as one
  entity. Pairing by definition pointer would emit add followed by remove and
  delete the VLAN.
- `NegateAs` forms (`no bridge <id>`, `no vlan <id> bridge <n>`) come
  directly from vendor documentation.
- 4-byte ASN (1..4294967295) plus asdot spelling (`HI.LO`, RFC 5396);
  asdot and plain spellings remain distinct text. Device emission of asdot
  notation is unverified.
- `max-paths {{mode}} {{paths}}` is the documented spelling. Do not use
  `maximum-paths`.
- CIDR-only interface addressing (`cidr4` rejects dotted masks).
- vlan range membership (`vlan 2-5 bridge 1 state enable`) is the
  composite-key membership fixture with an additional property. Trunk grammar
  has no bare-list spelling, so the `add`-form line is the canonical slot
  (self-union `ListContinues`), with `NegateAs` spelled as the remove form.

Unverified assumptions:

- Canonical indentation is probably one space because OcNOS derives from
  ZebOS. confetti renders a two-space canonical form. Its round-trip contract
  does not preserve original bytes.
- Default admin-state emission; emission order of `bridge` vs `vlan
  database` sections (schema declares `bridge` first to match
  definition-before-referrer ordering).
- `max-paths` numeric range left as bare `uint` (doc ranges were
  medium-confidence).
- Documentation reports that cmlsh cannot remove multiple configurations in
  one commit. An artifact with several `no` lines can require one removal per
  commit. Confetti does not model device commits.
- The fixture excludes VLAN reservation, hybrid ports, prefix-lists,
  peer-groups, unnumbered BGP,
  VRRP, MPLS.
- Global vlan-id uniqueness across bridges is device semantics the schema
  does not currently declare (`Unique` scope exists for it).

## Cross-cutting

- Round-trip produces canonical output, not byte-identical device output.
  Therefore, unverified indentation and emission order do not imply that
  confetti reproduces vendor output.
- Real-platform consumers and containerlab verification belong in a
  separate repo that imports confetti; in-repo fixtures stay minimal and
  test-driven.
