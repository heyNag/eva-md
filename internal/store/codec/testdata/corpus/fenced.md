---
type: procedure
title: "Run make check before a push"
status: stable
---
Run `make check` before a push.

```yaml
---
locked: true
## not a section: inside a fence
---
```

~~~~
## still not a section
```
## nor this: a backtick run does not close a tilde fence
~~~~

## Detail
   ```
   ## indented fence, not a section
   ```

## Evidence
[^e1]: claude-code:9c8b7a6d#5
