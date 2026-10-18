# Domain docs

## Before exploring

Read `GLOSSARY.md` at this repository's root and relevant decisions in `docs/adr/`.

If either is absent, proceed silently. The domain-modeling skill creates them lazily when terms or decisions are resolved.

## Layout

This repository uses a single-context layout:

- `GLOSSARY.md`: domain terms.
- `docs/adr/`: architecture decision records.

This is an independent repository. Consult the sibling repository only when the requested work requires coordinating their shared contract.

## Vocabulary and decisions

Use the glossary's terms in proposals, tickets, hypotheses, and test names. When a needed term is missing, first check the project's existing language; raise real gaps during domain modeling.

If a proposal contradicts an ADR, identify the ADR and explain why it merits reconsideration. Keep existing README and project guides authoritative for current behavior; these domain docs supplement them.
