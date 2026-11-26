# Chain graph

## Intent

Chains are fixed lists today: one opener, steps in order, one roll. A graph, where any sound can lead
to one of several next sounds, gives near-endless variety from a small config. Repetition and loops
are wanted; the walk must still always end.

## Approaches

- **A, picked:** at each hop roll "go on?", then pick one child by weight. One path per visit,
  same shape as `[[links]]`, easy to cap.
- **B, rejected:** every child rolls on its own and every passing one plays. Length grows
  exponentially per hop; the caps would carry the design.

## Config

```toml
[chaining]
chance   = 80    # % odds of going on at hop 0; 0-100, default 100
decay    = 0.7   # odds multiplier per hop; 0-1, default 1
max_hops = 8     # hard cap; 1-20, default 8

[[chains]]
sound = "knock"
next  = [
  { sound = "whos_there", weight = 70, after = 2 },
  { sound = "rimshot",    weight = 30 },
]
```

- Odds at hop *n* (0-based) are `chance × decay^n`; global, not per sound (user's call).
- Edges carry `weight` (> 0) and `after` (0-20 s, ms precision, as today).
- A sound without a `[[chains]]` entry ends the walk.

## Rules

- Load checks: ranges above, non-empty sound names, non-empty `next`, no sound with two
  `[[chains]]` entries. Cycles allowed; `max_hops` guarantees termination.
- Runtime: children with no file or blocked as NSFW are left out of the weighted pick; none left
  ends the walk.
- Every sound can be picked first (user's call): the `followers` exclusion is removed, from
  `PickOther` too.
- The old `steps` format is replaced outright: no character profile uses it. Example profile,
  wiki `Profile.md`, `Character-design.md`, `Architecture.md`, `Home.md` and tests follow.

## Left to the builder

Name and placement of the max-hops constant (next to `MaxAfter`).
