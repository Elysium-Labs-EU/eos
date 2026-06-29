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

## Avoid

| Don't | Why |
|-------|-----|
| Methods that mutate receiver silently | hidden state |
| Package-level vars read implicitly | hidden dependency |
| Interfaces >5 methods | hard to compose |
| `*SmallStruct` with no nil case | use value |
| Deep embedding | obscures data origin |
| Nil pointer as "empty" config | use zero value |
