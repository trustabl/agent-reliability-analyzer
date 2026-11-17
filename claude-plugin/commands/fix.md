---
description: Scan with Trustabl, then apply the fixes it suggests
argument-hint: "[path, or a severity such as high]"
---

Fix agent reliability and safety weaknesses that Trustabl finds.

`$ARGUMENTS` may name a path to scan, a minimum severity to act on, or nothing.
Default to the current working directory and to findings of medium severity and
above.

Work in this order:

1. **Scan first**, the same way as `/agent-reliability-analyzer:scan`:
   `"$TRUSTABL_BIN" scan <target> --format json`, with `.` unless the
   user named a target.
2. **Check the inventory** before touching anything. If the tool and agent
   counts look wrong, stop and say so. Applying fixes from a scan that was
   pointed at the wrong place is worse than doing nothing.
3. **Apply the fixes**, using the `trustabl-enrich` skill, which is driven by
   each finding's own explanation and suggested fix.
4. **Re-scan** and report what changed: which findings are gone, which remain,
   and the inventory counts again so it is clear the same thing was measured.

Two things to hold to while fixing:

- **Show the diff before writing**, and keep each change to what the finding
  actually calls for. Do not tidy surrounding code in the same pass, because it
  makes the fix impossible to review.
- **Leave anything you are unsure about.** Some findings need a judgement about
  the system that the scanner cannot make, such as which hosts a tool should be
  allowed to reach. Report those and say why you left them rather than guessing
  at a value.

To see findings without changing any files, use `/agent-reliability-analyzer:scan`.
