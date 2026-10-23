# 20: Published JSON Schema

Status: ready-for-human

Type: HITL

## Parent

[PRD: Engine core — manifest to verified Environment](../PRD.md)

## What to build

A JSON Schema for the manifest, generated from the spec types rather than maintained by hand, so it cannot drift from what the decoder actually accepts. It gives authors editor autocomplete and gives CI a lint.

**This slice is HITL** because it needs a decision that is still open: `prd.md` records the product name and the final API group as undecided, with `sim.dev` as a placeholder, to be settled before the schema is published. The generated schema embeds the group, so the answer has to exist first. Earlier slices proceed on the placeholder; this one cannot.

## Acceptance criteria

- [ ] The product name and the final API group are decided and recorded in `prd.md`
- [ ] A JSON Schema for the manifest is generated from the spec types, not hand-maintained
- [ ] A test asserts the schema accepts exactly what the decoder accepts: every valid manifest in the corpus validates against it, and every invalid one fails
- [ ] The schema is published in the repository at a stable location
- [ ] An editor configured against it autocompletes fields and flags unknown ones
- [ ] CI lints every manifest in the repository against it
- [ ] Schema generation runs in CI, so it cannot drift from the spec types

## Blocked by

- [04: Full validation and the 30-manifest negative corpus](./04-validation-negative-corpus.md)
