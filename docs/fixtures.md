# Fixture lineage and grammar assumptions

The schemas in `internal/fixture/` describe imaginary platforms derived from
real vendor grammars. Verify their documented assumptions before treating
fixture behavior as vendor-accurate.

| Fixture | Lineage | Grounding |
|---|---|---|
| `internal/fixture/alpha` | Cisco NX-OS running-config slice | Real reference configs |
| `internal/fixture/beta` | IPInfusion OcNOS 6.x/7.0 (cmlsh) | Public documentation only; no running configuration was available |

The schemas remain separate for three reasons. Alpha protects `router bgp`,
but beta removes it in golden tests. Alpha uses a 16-bit ASN, while beta uses
a 4-byte ASN with asdot notation. Strict unknown-input tests also require a
foreign grammar.

## alpha (NX-OS lineage)

Constructs supported by reference configurations or documentation:

- Three references come from real configurations: `switchport access vlan`
  to `vlan`, `vrf member` to `vrf context`, and `neighbor … route-map` to
  `route-map`.
- `address-family {{afi}}` and `{{afi}} {{safi}}` share children through
  `Adopt`. Both are needed because reference output includes
  `address-family ipv4` without a SAFI token.
- `banner motd {{delim}}` provides the multi-line block-capture fixture.
  Beta documentation describes only single-line banners.
- `interface {{name:ethport}}` models physical-port resets with
  `NegateDefault`; `{{name:ifname}}` models logical-port deletion. The
  templates have equal literal specificity, so the physical definition must
  appear first.
- NX-OS emits both `vlan {{ids}}` membership lines and per-VLAN sections,
  with the membership line first. This requires a complete level scan before
  folding.
- Trunk allowed-VLAN grammar includes the documented keyword
  spellings (`none`/`all`/`except`), `add` continuation lines, and
  add/remove delta forms.
- Import drop rules (`!` comments, `version`, `exit`, config preamble)
  mirror real `show running-config` noise.
- Reference configurations print neither `vlan 1` nor `vrf context default`,
  so `Baseline()` declares both.

Unverified behavior and fixture limits:

- `default interface <name>` reset form is documented but was not verified on
  a real image.
- Default administrative state is not modeled because it depends on the
  platform and port.
- `asn` is 16-bit here; beta uses 4-byte ASNs.
- Route-map `match`/`set` children have no cited vendor source. Only the
  header and reference target come from reference configurations.
- The `feature bgp` → `router bgp` dependency is fixture data with no
  reference source.
- The reserved VLAN range (3968–4095) is not modeled; list keywords use
  the domain `1-4094`.
- `Baseline()` models VLAN 1 and the default VRF as device-provided, and
  `vrf member default` as their referrer. The device behavior was not
  confirmed on a lab image.
- Single-line banners (`banner motd ^text^`) are not modeled.

## beta (OcNOS lineage)

All beta behavior comes from docs.ipinfusion.com. Verify the complete grammar
against a device or a containerlab `ipinfusion_ocnos` image. The following
constructs have documentation support:

- VLANs are nested under `vlan database`, keyed by `(id, bridge)`, with
  mutable `state`. This tests a keyed-leaf Modify. The mode has no
  `no vlan database` command, so `ClearOnRemove` sets `EmptyOnRemove` to
  remove its children individually. This is the only fixture using it.
- Named and unnamed VLAN templates share Kind+Key, so a rename pairs as one
  entity. Pairing by definition pointer would emit add followed by remove and
  delete the VLAN.
- `NegateAs` forms (`no bridge <id>`, `no vlan <id> bridge <n>`) come
  directly from vendor documentation.
- ASNs support 4-byte values (1..4294967295) and asdot spelling (`HI.LO`,
  RFC 5396). Asdot and plain spellings remain distinct text. Device emission
  of asdot notation is unverified.
- `max-paths {{mode}} {{paths}}` is the documented spelling. Do not use
  `maximum-paths`.
- Interface addresses use CIDR; `cidr4` rejects dotted masks.
- VLAN range membership (`vlan 2-5 bridge 1 state enable`) is the
  composite-key membership fixture with an additional property. Trunk grammar
  has no bare-list spelling, so the `add`-form line is the canonical slot
  (self-union `ListContinues`), with `NegateAs` spelled as the remove form.

Unverified assumptions:

- Canonical indentation is unverified. OcNOS derives from ZebOS and may use
  one space. confetti renders two spaces and does not preserve original bytes.
- Default administrative-state output and section order are unverified.
  The schema declares `bridge` before `vlan database` to place definitions
  before references.
- `max-paths` uses bare `uint`; its documented ranges are unverified.
- Documentation reports that cmlsh cannot remove multiple configurations in
  one commit. An artifact with several `no` lines can require one removal per
  commit. Confetti does not model device commits.
- The fixture excludes VLAN reservation, hybrid ports, prefix-lists,
  peer-groups, unnumbered BGP, VRRP, and MPLS.
- The fixture does not declare global VLAN-ID uniqueness across bridges.
  Such a rule would use `Unique("id").ScopedByDevice()`.

## Cross-cutting

- Round-trip produces canonical output, not byte-identical device output.
  Therefore, unverified indentation and emission order do not imply that
  confetti reproduces vendor output.
- Production schemas and containerlab verification belong in downstream
  repositories that import confetti. Keep these fixtures limited to test cases.
