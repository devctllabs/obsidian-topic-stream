# Topics

A Topic is a saved thematic subscription. Its scope combines optional Sources and Item kinds;
an empty list means all Sources or kinds. Include and Exclude are natural-language criteria.
Ordinary mode favors broader matching; strict mode favors precision.

Creating a Topic records its first immutable semantic revision. Changing its Source scope, Item-kind
scope, Include, Exclude, or mode creates a new revision. Renaming, enabling, and disabling leave the
semantic revision unchanged, preserving the meaning of historical recommendations and Digest Issues.

Users can list, create, update, and delete Topics. Editing preserves current Topic Membership
Corrections unless the user explicitly clears them. Corrections remain separate from Personal
Feedback and help calibrate automatic membership in the background.

Enabled Topics prepare candidate lists asynchronously. Changes that make a list incompatible
request a replacement; disabling or deleting a Topic releases its list. Preparation never publishes
cards by itself. Topic edits affect future admission rather than reclassifying old Feed Publications.

Add to topic, Wrong topic, and clearing corrections also change displayed membership of already
published Items without changing their publication time or order. See [Feed and feedback](feed-and-feedback.md)
and [ranking behavior](ranking.md). Calibration and projection mechanics are documented in the
[ranking pipeline](../../architecture/ranking-pipeline.md).
