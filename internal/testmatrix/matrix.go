package testmatrix

import (
	"context"

	"golang.org/x/sync/errgroup"
)

// Options controls a matrix run.
type Options struct {
	RunID          string // unique per invocation, used to name clones
	SuiteFilter    string // if set, only run the suite with this name
	MaxConcurrency int    // 0 = unlimited (one goroutine per target/suite pair)
	KeepOnFailure  bool   // leave a failed suite's clone running for debugging
	IncludeNightly bool   // also run suites marked Nightly
}

type pair struct {
	target Target
	suite  Suite
}

// pairs expands cfg into the concrete (target, suite) combinations opts
// selects, honoring each suite's Only allowlist and Nightly/SuiteFilter
// options.
func pairs(cfg Config, opts Options) []pair {
	var out []pair
	for _, s := range cfg.Suites {
		if s.Nightly && !opts.IncludeNightly {
			continue
		}
		if opts.SuiteFilter != "" && s.Name != opts.SuiteFilter {
			continue
		}
		for _, t := range cfg.Targets {
			if s.AppliesTo(t.Name) {
				out = append(out, pair{target: t, suite: s})
			}
		}
	}
	return out
}

// RunMatrix runs every selected (target, suite) pair concurrently and
// returns one Result per pair. A single pair failing does not stop the
// others — failures are reported in the returned Results, not as a returned
// error.
func RunMatrix(ctx context.Context, o *Orb, cfg Config, opts Options) []Result {
	work := pairs(cfg, opts)
	results := make([]Result, len(work))

	g, gctx := errgroup.WithContext(ctx)
	if opts.MaxConcurrency > 0 {
		g.SetLimit(opts.MaxConcurrency)
	}

	for i := range work {
		g.Go(func() error {
			p := work[i]
			results[i] = RunTarget(gctx, o, p.target, p.suite, opts.RunID, opts.KeepOnFailure)
			return nil
		})
	}
	_ = g.Wait()

	return results
}
