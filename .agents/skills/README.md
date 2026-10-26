# Skills

Procedures an assistant follows when working on this package. They live in
`.agents/skills/<name>/SKILL.md`; the `name` in the frontmatter equals the
directory name.

| skill | when it fires |
| --- | --- |
| `claude-release` | the gates, the manifest, a dependency, a version, a tag |
| `claude-vault-notes` | writing the note, the gap or the journal entry, when this checkout sits inside the Arandu Obsidian vault |
| `claude-package` | installing and wiring this package **into an application** |

`claude-package` has `metadata.audience: app`, which is what `aru skills:sync`
reads to copy it into an application that requires this module.
