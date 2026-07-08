# Style

Go-FP: pure functions, explicit data flow, no hidden state.

## Rules

**Structs = data. Functions transform data.**
Add methods only to implement stdlib interfaces (`String()`, `Error()`, `io.Writer`).

**Value semantics for config/data.**
Pointer only when nil signals meaningful absence or mutation is intentional.

**Mutually exclusive pointer fields = sum type Go can't express.**
Explicit `nil` + comment on each branch. Omission looks like a mistake.
```go
return config.DaemonConfig{
    Standalone: nil,                        // not standalone
    Systemd:    &config.SystemdConfig{...},
}
```

**Narrow inputs — pass only what function needs.**
```go
func NewDaemonLogger(cfg config.DaemonLogConfig) (*Logger, error)  // not SystemConfig
```

**Pure core, effects at edges.**
```go
func buildSystemdUnit(cfg config.SystemdConfig) string  // pure
func writeSystemdUnit(path, content string) error       // effect at boundary
```

I/O (file stat, env read, network) belongs at the call site, not inside constructors. Constructors take already-resolved values.
```go
isManaged := config.IsSystemdManaged(...)   // I/O at boundary
cfg := newDaemonConfig(baseDir, isManaged)  // pure builder, no I/O inside
```

**Explicit composition, not embedding.**
```go
type DaemonConfig struct {
    Log        DaemonLogConfig         // not embedded — origin visible
    Standalone *StandaloneDaemonConfig
}
```

**Small interfaces, defined at consumption point.**
1–3 methods. Define where used, not where implemented.

**Errors as values, wrapped with context.**
```go
return nil, fmt.Errorf("starting daemon: %w", err)
```

**Constructors return concrete types.**
```go
func newDaemonConfig(...) config.DaemonConfig  // value, not pointer
```

## Subprocess lifecycle

**Kill before Wait on every exit path.**
Any branch that calls `Kill()` before `Wait()` means all branches must.
`cmd.Wait()` blocks indefinitely if process ignores signal.
```go
// Wrong: stopCh path hangs if plugin ignores stdin close
case <-stopCh:
    _ = stdin.Close()
    _ = cmd.Wait()

// Right:
case <-stopCh:
    _ = cmd.Process.Kill()
    _ = cmd.Wait()
```

**`defer close(doneCh)` registers after early returns → deadlock.**
Register defer first, or close explicitly on every early exit.
```go
// Wrong: caller blocks forever on <-doneCh
if bad { return }
defer close(s.doneCh) // never reached

// Right:
if bad { close(s.doneCh); return }
defer close(s.doneCh)
```

**One write path into ordered queue.**
Two paths (channel + direct push) invert order under backpressure.
Use mutex-safe container directly; no intermediate channel needed.

**Concurrent drain ordering on shutdown.**
When stopCh fires: if goroutine A feeds buffer and goroutine B drains it,
there is no ordering — B may finish before A pushes last records.
Solution: eliminate one goroutine, or signal A→done before B reads final pass.

**`time.After` in loop select leaks timer.**
Use `time.NewTimer` + `defer t.Stop()` or `t.Stop()` on early select exit.
```go
t := time.NewTimer(delay)
select {
case <-stopCh:
    t.Stop(); return
case <-t.C:
}
```

## Avoid

| Don't | Why |
|-------|-----|
| Methods that mutate receiver silently | hidden state |
| Package-level vars read implicitly | hidden dependency |
| Interfaces >5 methods | hard to compose |
| `*SmallStruct` with no nil case | use value |
| Deep embedding | obscures data origin |
| Nil pointer as "empty" config | use zero value |
