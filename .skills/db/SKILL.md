---
name: db
description: Database workflow — Conventions and sqlite tools
---

For SQL migrations and queries sort tables and columns lexigraphically when possible.

For IDs use `INTEGER PRIMARY KEY`. 

For timestamps use `TIMESTAMP` and `NOT NULL` and `DEFAULT CURRENT_TIMESTAMP` where applicable. Track `created_at`, `updated_at`, `archived_at`, `trashed_at`.

We strongly favor soft archive and trash over hard delete. Use foreign keys and `ON DELETE RESTRICT` to further prefer hard deletes.

Use as simple names for objects and fields as possible. 

Consider using web standards. For example we use W3C Activity Streams 2.0 to model our "activities" table with "actor", "object", "target" "id" and "type" fields.
