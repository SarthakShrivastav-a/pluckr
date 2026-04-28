---
description: Subscribe to a docs source via pluckr (website, llms.txt, github repo, or local folder).
---

Run `pluckr add` with the spec the user provides. The kind is auto-detected:

  - `https://...` → website (or `llms_txt` if URL ends in `/llms.txt`)
  - `owner/repo[@ref][/subdir]` → github
  - filesystem path → local

After adding, run `pluckr pull <name>` to fetch the content unless the user asked for `--pull`.

If the user passes a name with `--name`, honor it; otherwise let pluckr derive one from the spec.

Spec: !ARGUMENTS
