# Changelog

All notable changes to eos are documented here.

## [0.0.11-rc.25] - 2026-07-13

### Bug Fixes
- Render starting status distinct from unknown ([`261a837`](https://codeberg.org/Elysium_Labs/eos/commit/261a8372168e7c1533710801940b3ee1c5da3726))
- Return proper exit codes from daemon/system commands ([`ae29a09`](https://codeberg.org/Elysium_Labs/eos/commit/ae29a0931d37800db08dcf9977cc6864d1d01eda))
- Recover from stale Starting process history entries ([`7950fa6`](https://codeberg.org/Elysium_Labs/eos/commit/7950fa67e9dd29439b9c4acd5cc0a7f93bb3b6a3))
- Stop GetBaseDir honoring SUDO_USER when not running as root ([`d1df1af`](https://codeberg.org/Elysium_Labs/eos/commit/d1df1afbd775b7cd3f1143c4b80f6f1acefaa215))


### Documentation
- Add rule on ambient lookups as hidden dependencies ([`34baf62`](https://codeberg.org/Elysium_Labs/eos/commit/34baf62ff55ad3aa4750bd6762f746fc348cce73))


### Maintenance
- Default to help target when make runs with no args ([`7c7fcb0`](https://codeberg.org/Elysium_Labs/eos/commit/7c7fcb0e99c830a713e81ac0cdf577009b9d0845))
- Remove completed TEST_COVERAGE_TODO.md ([`cadd2d9`](https://codeberg.org/Elysium_Labs/eos/commit/cadd2d93cdf99507c56ef78a09c5ab22769f22b7))


### Miscellaneous
- Merge pull request 'fix(sinks): flush plugin stdin after each record' (#93) from fix/sink-record-flush into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/93 ([`5fe66a6`](https://codeberg.org/Elysium_Labs/eos/commit/5fe66a64e734a11ebbb3986c721c8a23b89e2f6c))
- Merge remote-tracking branch 'origin/main' into worktree-test-coverage-review ([`345783b`](https://codeberg.org/Elysium_Labs/eos/commit/345783b1a0c1e57f5b1679d6d1da5286b7b5d54c))
- Drop arrow glyphs from CLI hint text ([`e4be32d`](https://codeberg.org/Elysium_Labs/eos/commit/e4be32dbc8eb01da810dc0badd6bf0ae3591e150))
- Merge pull request 'worktree-test-coverage-review' (#94) from worktree-test-coverage-review into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/94 ([`20ca697`](https://codeberg.org/Elysium_Labs/eos/commit/20ca697321b54105262f28422103e960c2673292))

## [0.0.11-rc.24] - 2026-07-10

### Bug Fixes
- Correct log rotation, IPC arg, and error-wrap bugs found in review ([`f30e751`](https://codeberg.org/Elysium_Labs/eos/commit/f30e75105a8cf8b49fdfd177a74e676f0e1f07f9))
- Add panic isolation and port-reachability check to health monitor ([`d612680`](https://codeberg.org/Elysium_Labs/eos/commit/d612680f2964653b95533abb60ebb7480a1f1aa2))
- Correct dead sentinel check and mistested command in api info ([`2684ada`](https://codeberg.org/Elysium_Labs/eos/commit/2684ada70a0642c990c0ee1c0321973c9ac2efd0))
- Prevent panic when api run is called with no args or -f ([`560e529`](https://codeberg.org/Elysium_Labs/eos/commit/560e529331688851fd746b0b94692c40aa92676d))
- Wire ValidateServiceConfig into add/run/update, matching api validate ([`494ded8`](https://codeberg.org/Elysium_Labs/eos/commit/494ded8638e6810071becc9ab0ab906216e79eae))
- Wire ValidateServiceConfig into interactive add, matching api add ([`0c9a226`](https://codeberg.org/Elysium_Labs/eos/commit/0c9a226863b76ee43fb4e4c36cdf5152f9693a84))
- Make interactive add return a real error on failure ([`8af316a`](https://codeberg.org/Elysium_Labs/eos/commit/8af316ae3ce1609445c3a8fe5bc434d3df4f8703))
- Convert remaining interactive commands from Run to RunE ([`a0fa671`](https://codeberg.org/Elysium_Labs/eos/commit/a0fa671f6aa9c9296cd28ed5b863762c90ac773e))
- Route status table/watch-frame output through cmd, not raw stdout ([`f12135d`](https://codeberg.org/Elysium_Labs/eos/commit/f12135d8cc2b89382963e0a731b4868f8ffc9ec5))
- Correct combined-log merge order and surface tail failures ([`b33bcc2`](https://codeberg.org/Elysium_Labs/eos/commit/b33bcc2aee3475c800ce9c2ace8623b94ed0bd21))
- Honor status in DetermineProcessMemoryInMbAPI, stop PromptConfirm dropping answers ([`8245299`](https://codeberg.org/Elysium_Labs/eos/commit/824529991e07dc5b70b0d268c15c4722ee4e46c1))
- Preserve correct HOME for privilege-dropped detach child ([`c463237`](https://codeberg.org/Elysium_Labs/eos/commit/c46323724ecd18bbd337c0b46c409b53f442925f))
- Stop startup/unstartup hardcoding the real "eos" unit ([`ffb4dfa`](https://codeberg.org/Elysium_Labs/eos/commit/ffb4dfa495119d2b910b1703b413f76f9944b97d))
- Flush plugin stdin after each record ([`451f1a2`](https://codeberg.org/Elysium_Labs/eos/commit/451f1a288a25ea45ce0d1bae54fca431d70b84d7))


### Miscellaneous
- Merge pull request 'fix(daemon): warn when user bus is down before printing systemctl/journalctl hints' (#92) from fix/daemon-info-bus-warning into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/92 ([`43c2562`](https://codeberg.org/Elysium_Labs/eos/commit/43c256224fc4388eb13913698a29b86db06d77cf))


### Testing
- Clean up stale comments and misleading assertion messages ([`e3bd399`](https://codeberg.org/Elysium_Labs/eos/commit/e3bd399137a70c6faa1a058f8ba81968b6525f1a))
- Clarify regression comments with git history ([`561eef8`](https://codeberg.org/Elysium_Labs/eos/commit/561eef8d7b886907fbe3ba1cb4085617330c46e3))
- Remove duplicate assertion block in integration CRUD test ([`da8acbe`](https://codeberg.org/Elysium_Labs/eos/commit/da8acbefa222c3b7ff3211dceb070122ce3acca7))
- Fix stray error message and clarify FK-pragma test scope ([`b5c6976`](https://codeberg.org/Elysium_Labs/eos/commit/b5c6976c81bcc6920156936821c460bb59058217))
- Document PGID derivation in benchmark ([`07340e6`](https://codeberg.org/Elysium_Labs/eos/commit/07340e64f9b7b0bef66101dcdc5ea5664bbe67e7))
- Document synthetic /proc status fixture in benchmark ([`d8249e8`](https://codeberg.org/Elysium_Labs/eos/commit/d8249e8c4d84a93312e5a396767545a2dad96414))
- Clarify SUDO_USER test only proves code path, not resolution ([`2c13f08`](https://codeberg.org/Elysium_Labs/eos/commit/2c13f08e39064909958388c38a62fba470752f80))
- Document per-line timestamp behavior in TimestampWriter test ([`8ba844a`](https://codeberg.org/Elysium_Labs/eos/commit/8ba844a0d5706700f2c6574371e8d45e4c3a6ea4))
- Add real tests, replacing dead commented-out stubs ([`dc20eae`](https://codeberg.org/Elysium_Labs/eos/commit/dc20eae5a0a9f6401bf476e81207277a59a12a7e))
- Remove dead code and document shared API test fixtures ([`bead472`](https://codeberg.org/Elysium_Labs/eos/commit/bead472d1680cf70f7d4fc4ad12be81c58d37802))
- Trim redundant comments, clarify directory-add test intent ([`db99209`](https://codeberg.org/Elysium_Labs/eos/commit/db9920904de02ac8994612d4ef1b3bffd6ece476))
- Document startServiceForLogsTest as a shared logs-test fixture ([`7d361d3`](https://codeberg.org/Elysium_Labs/eos/commit/7d361d311ec444f38bf1a35995f86418e025a442))
- Note cross-file helper reuse in api remove test ([`80541c9`](https://codeberg.org/Elysium_Labs/eos/commit/80541c95e8bf1fc357b6a242da12d25e3633a07b))
- Clarify comments in api status tests ([`e56fdb0`](https://codeberg.org/Elysium_Labs/eos/commit/e56fdb0bc47a3eca36f0b5bdf6aeaede3c45d879))
- Trim redundant comments, document shared stop-test fixture ([`f43c9e7`](https://codeberg.org/Elysium_Labs/eos/commit/f43c9e7c63776fe6acb3d1d8f878c9f70c467776))
- Correct false claim in api update test comment ([`ba38596`](https://codeberg.org/Elysium_Labs/eos/commit/ba38596bbbbb4da709335dfcd7844f961e56a75a))
- Finish api validate comment fixes from task 34 ([`aca2185`](https://codeberg.org/Elysium_Labs/eos/commit/aca2185de4a841d9674ba8b6119e9b544b8e3f45))
- Correct TODO labels claiming a mock manager is required ([`687cdec`](https://codeberg.org/Elysium_Labs/eos/commit/687cdec4f0c3dcd28a2962c02e53a4dd803832ec))
- Rename misleading test, add coverage for run -f edge cases ([`19eaed0`](https://codeberg.org/Elysium_Labs/eos/commit/19eaed078d0c90309a7bc4193425c605bfa887ce))
- Clean up stop_test.go and add coverage for --force, no-mock gaps ([`745c60b`](https://codeberg.org/Elysium_Labs/eos/commit/745c60b2052a138b14ef681f300459d905eccda7))
- Remove dead test, add coverage for info.go's untested states ([`7523948`](https://codeberg.org/Elysium_Labs/eos/commit/7523948cf6df34daeb9db0e7e224195b643d77e9))
- Add coverage for print.go's untested render funcs ([`21b27f9`](https://codeberg.org/Elysium_Labs/eos/commit/21b27f9d8639b5db683efbc63f1c15d5b361b618))
- Add coverage for helpers.go's untested funcs ([`a82cdbd`](https://codeberg.org/Elysium_Labs/eos/commit/a82cdbdc353fe4b5f45a4781d57fe30476c5e2ed))
- Add coverage for completions.go's ServiceNameCompletions ([`d9685f3`](https://codeberg.org/Elysium_Labs/eos/commit/d9685f31d88b86a9c6745c68b93e69fd8f867e27))
- Add coverage for status.go's renderWatchFrame ([`fde9d15`](https://codeberg.org/Elysium_Labs/eos/commit/fde9d1582084b2163d9d8f968bc91eab511af8a2))
- Add coverage for daemon.go's standalone controller ([`44505f8`](https://codeberg.org/Elysium_Labs/eos/commit/44505f8a618c1fa4ccd21d628fcc98ced0afb808))
- Add coverage for api_daemon_logs.go ([`040fdb3`](https://codeberg.org/Elysium_Labs/eos/commit/040fdb3caf3dcc62780668f2722483d7aac27004))
- Add coverage for api_info.go's compileProcessInfoObject ([`31ea478`](https://codeberg.org/Elysium_Labs/eos/commit/31ea478d221118d5858eeda8e7abf6c394d1d098))
- Add coverage for system.go's uninstall path ([`30b02dd`](https://codeberg.org/Elysium_Labs/eos/commit/30b02dd5afb02ec762a55690cbdc3afaaf299fc9))
- Add coverage for remove.go and update.go gap cases ([`a51003b`](https://codeberg.org/Elysium_Labs/eos/commit/a51003b2043bda310285fad778f610603029e0cd))
- Add coverage for config.go's ValidateServiceConfig ([`4c14ffc`](https://codeberg.org/Elysium_Labs/eos/commit/4c14ffcef87cbc675f1ab3f99e9e24b4b4d1e51d))
- Add coverage for executor.go and daemon_manager.go gaps ([`694165a`](https://codeberg.org/Elysium_Labs/eos/commit/694165ac70680dfda087fc0d45c2ae5f1ef6587a))
- Add coverage for local_manager.go's WaitPipes, RestartService, UpdateServiceCatalogEntry ([`3215335`](https://codeberg.org/Elysium_Labs/eos/commit/3215335f01b8f9de0f89ccd4524e9f639660f5b1))
- Add coverage for service_log.go's health-log write path ([`2644366`](https://codeberg.org/Elysium_Labs/eos/commit/26443669e716ba2617cfd01a596e85f9e5780cf0))
- Add coverage for health_monitor.go's restartOnMemoryThreshold ([`92ab15d`](https://codeberg.org/Elysium_Labs/eos/commit/92ab15d97ad52e3693c1ebb877cef3945a99638f))
- Add coverage for evaluateMemoryThresholds and dispatchMemoryAction ([`d20fd4f`](https://codeberg.org/Elysium_Labs/eos/commit/d20fd4fb88ece51e499f56e24be58d6a134924b2))
- Add direct coverage for standalone daemon lifecycle funcs ([`473ddc8`](https://codeberg.org/Elysium_Labs/eos/commit/473ddc8a8eceab1315fe3a0f9615a5a2bdd6f0d6))

## [0.0.11-rc.23] - 2026-07-10

### Bug Fixes
- Reject stale XDG_RUNTIME_DIR owned by another user ([`abaa999`](https://codeberg.org/Elysium_Labs/eos/commit/abaa99971aec347ef08081c553518ec0c0591725))
- Resolve XDG_RUNTIME_DIR ownership against target user, not process uid ([`ad366db`](https://codeberg.org/Elysium_Labs/eos/commit/ad366db4f60c7d9c15598b3cd7ca4c4d4b2891b1))
- Warn when user bus is down before printing systemctl/journalctl hints ([`6da2343`](https://codeberg.org/Elysium_Labs/eos/commit/6da234384a48f488ad258d605a5dace43970b7cd))


### Miscellaneous
- Merge pull request 'fix/daemon-stop-bus-uid-check' (#91) from fix/daemon-stop-bus-uid-check into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/91 ([`f6ce34b`](https://codeberg.org/Elysium_Labs/eos/commit/f6ce34bd31d979d33daee6577a6c7bbd229f4355))

## [0.0.11-rc.22] - 2026-07-10

### Bug Fixes
- Auto-heal user bus before systemctl stop ([`c2a2911`](https://codeberg.org/Elysium_Labs/eos/commit/c2a29111e0cb5fd2a0040916a007c41cb7a7e521))


### Miscellaneous
- Merge pull request 'fix(daemon): auto-heal user bus before systemctl stop' (#90) from fix/daemon-stop-bus-autoheal into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/90 ([`6b1a808`](https://codeberg.org/Elysium_Labs/eos/commit/6b1a80815511eb9cd21912a20a4675d314a994b5))

## [0.0.11-rc.20] - 2026-07-10

### Features
- Auto-refresh installed shell completions on upgrade ([`f0f7bc4`](https://codeberg.org/Elysium_Labs/eos/commit/f0f7bc446bf633c8257077a667508330ea25ded6))


### Miscellaneous
- Merge pull request 'feat(completion): auto-refresh installed shell completions on upgrade' (#88) from feat/completion-auto-refresh into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/88 ([`e5d02da`](https://codeberg.org/Elysium_Labs/eos/commit/e5d02da9b3c8549f0ec448a24be35af528fe8674))
- Merge pull request 'refactor(monitor): split checkRunningProcess into focused helpers' (#89) from refactor/split-check-running-process into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/89 ([`63d48bb`](https://codeberg.org/Elysium_Labs/eos/commit/63d48bb046373f6fad3984809655f3442ddf7c0c))


### Refactoring
- Split checkRunningProcess into focused helpers ([`f777628`](https://codeberg.org/Elysium_Labs/eos/commit/f77762835b344c1119991a5ac1ac8936fce0326b))

## [0.0.11-rc.19] - 2026-07-10

### Bug Fixes
- Resolve systemd scope from installed unit, not caller uid ([`561ce22`](https://codeberg.org/Elysium_Labs/eos/commit/561ce22534a88e9e7487f7e4ee481439cb8e44cd))


### Miscellaneous
- Bumps deps to latest version ([`012370e`](https://codeberg.org/Elysium_Labs/eos/commit/012370e54009c2343399c4a2d95e010cd3c94b90))
- Merge pull request 'bumps deps to latest version' (#86) from bumps-deps-10-jul-26 into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/86 ([`ffa7154`](https://codeberg.org/Elysium_Labs/eos/commit/ffa715496c1b8c5f437aee114796473290180673))
- Merge pull request 'fix(daemon): resolve systemd scope from installed unit, not caller uid' (#87) from fix/daemon-user-systemd-scope into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/87 ([`2e819f4`](https://codeberg.org/Elysium_Labs/eos/commit/2e819f42ef27872a1e74d8f7f23f25972652b6ee))

## [0.0.11-rc.18] - 2026-07-10

### Bug Fixes
- Auto-heal user bus failure and wire --verbose into startup/unstartup ([`84f3524`](https://codeberg.org/Elysium_Labs/eos/commit/84f35241ae93ec9cadaafdec353acd7d7c302d9b))


### Miscellaneous
- Merge pull request 'fix: don't treat an already-dead daemon as a stop failure' (#82) from fix/stop-daemon-already-finished into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/82 ([`3a5108f`](https://codeberg.org/Elysium_Labs/eos/commit/3a5108f3df737bb653f841bf2e9f79cbc249f964))
- Merge pull request 'fix(system): auto-heal user bus failure and wire --verbose into startup/unstartup' (#84) from fix/system-startup-bus-verbose into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/84 ([`d0a5985`](https://codeberg.org/Elysium_Labs/eos/commit/d0a5985ec250bdb3b22b6b576e6a9259d3b1bba3))
- Service Orchestration Tool -> Service Supervisor ([`81726a8`](https://codeberg.org/Elysium_Labs/eos/commit/81726a8f533ed66f2af0f0045e602902ee8ec8a5))
- Merge pull request 'Service Orchestration Tool -> Service Supervisor' (#85) from tweak-tagline-naming into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/85 ([`e62a3ad`](https://codeberg.org/Elysium_Labs/eos/commit/e62a3ad025cdb71628449aeba4c8ff3a725bd931))

## [0.0.11-rc.15] - 2026-07-08

### Bug Fixes
- Don't treat an already-dead daemon as a stop failure ([`9e90174`](https://codeberg.org/Elysium_Labs/eos/commit/9e90174170e6c1b4ddb591a181e93d9df882f5c2))

## [0.0.11-rc.14] - 2026-07-08

### Bug Fixes
- Fix renderServiceLogLine call signature in tests ([`b406dfe`](https://codeberg.org/Elysium_Labs/eos/commit/b406dfe7038bb4a1cbe9e132667b4f902178dceb))
- Route JSON output to stdout via OutOrStdout() ([`743da99`](https://codeberg.org/Elysium_Labs/eos/commit/743da99222a25a87588cf78cadf8d49f08b33b02))
- Make runtime type and path independently optional ([`9c41252`](https://codeberg.org/Elysium_Labs/eos/commit/9c412522ce6d74dc1f1021593981285963ad7efa))
- Fix 7 correctness bugs in sink process lifecycle ([`2c27c93`](https://codeberg.org/Elysium_Labs/eos/commit/2c27c93dd495b4fd12041de44666060751919368))


### Documentation
- Publish schema URL and add yaml-language-server hint ([`e58d024`](https://codeberg.org/Elysium_Labs/eos/commit/e58d024933785a5e2901fb2f36ac42339c921f81))
- Use consistent "service supervisor" terminology ([`34e825d`](https://codeberg.org/Elysium_Labs/eos/commit/34e825de6a5ba3afb53bc7d99502fd295d43c0a7))
- Add GitHub Actions deploy section ([`8ead815`](https://codeberg.org/Elysium_Labs/eos/commit/8ead8150530208c2f0a9ec31833075b2210e6f6e))
- Add CONTRIBUTING guide and PR template ([`c55a8c3`](https://codeberg.org/Elysium_Labs/eos/commit/c55a8c3d8e26457623da2b843f982248745324c5))
- Add log sinks section; replace em-dash with semicolon ([`0684056`](https://codeberg.org/Elysium_Labs/eos/commit/0684056c8c2931caf59856f05444c1ef04493f46))


### Features
- Introduce Monitor and monitorManager interfaces ([`8d89c4a`](https://codeberg.org/Elysium_Labs/eos/commit/8d89c4a1392a0c966a9d9fe81b054d9e97ac7947))
- Configurable check interval test coverage and docs ([`c8a6412`](https://codeberg.org/Elysium_Labs/eos/commit/c8a64122813673d0da6574d37c50dc5d9b2595a1))
- Add eos init command ([`9968f1e`](https://codeberg.org/Elysium_Labs/eos/commit/9968f1e666dfe7944fad88e139589c62dc0dfd24))
- Suggest shell completion setup after install ([`be97083`](https://codeberg.org/Elysium_Labs/eos/commit/be970833fee34eb8ac8fd51bc55842f743801423))
- Add full subcommand coverage with tests ([`45dfa00`](https://codeberg.org/Elysium_Labs/eos/commit/45dfa00b3f16df3c2a93302cfb49967f5f2c453c))
- Add interactive shell completion installer ([`a639f1e`](https://codeberg.org/Elysium_Labs/eos/commit/a639f1ee3f929a505b584e81a879afbec55cf561))
- Add log sink plugin system ([`0c13586`](https://codeberg.org/Elysium_Labs/eos/commit/0c135865385e9146cb3df62e15d3522711848d8d))
- Wire mode, address, and error log routing through sink pipeline ([`5e943b9`](https://codeberg.org/Elysium_Labs/eos/commit/5e943b91e61e6b72ed8d61590a4b037bb1e288f6))


### Maintenance
- Remove bundled logbench plugin; moved to eos-plugins repo ([`d70c440`](https://codeberg.org/Elysium_Labs/eos/commit/d70c440a7c36baeeafb3391ecce60f599ea34e5a))
- Merge main; keep Log Sinks and Deploy with GitHub Actions sections ([`9fdac33`](https://codeberg.org/Elysium_Labs/eos/commit/9fdac3386c1185e43bbfef17dd88e79973d0984a))


### Miscellaneous
- Merge pull request 'feat/readme-env-config' (#68) from feat/readme-env-config into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/68 ([`684aa0a`](https://codeberg.org/Elysium_Labs/eos/commit/684aa0a6f69a1fbd34d4ad31a90dd9e0799a48d2))
- Merge pull request 'feat(monitor): introduce Monitor and monitorManager interfaces' (#69) from worktree-feat+monitor-interface into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/69 ([`9cc03d8`](https://codeberg.org/Elysium_Labs/eos/commit/9cc03d86dab9e0a0cb66831df0b4d00d805a1ce6))
- Merge pull request 'feat(monitor): configurable check interval test coverage and docs' (#70) from worktree-feat+health-monitor-configurable-interval into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/70 ([`61296db`](https://codeberg.org/Elysium_Labs/eos/commit/61296dbbd4249963600aad340e4992663958840f))
- Merge pull request 'docs: publish schema URL and add yaml-language-server hint' (#72) from worktree-feat+issue-19-schema-docs into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/72 ([`f5daa05`](https://codeberg.org/Elysium_Labs/eos/commit/f5daa05c987491ecb8f9c128babe39891aa609ba))
- Merge pull request 'worktree-refactor+require-manager' (#73) from worktree-refactor+require-manager into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/73 ([`51e3aae`](https://codeberg.org/Elysium_Labs/eos/commit/51e3aaef454fc0383d63ce77d4dd02301ec2bdab))
- Replace em-dashes with semicolons in comments ([`cccdb85`](https://codeberg.org/Elysium_Labs/eos/commit/cccdb85686158a4c319bc9878ebc86004bc8dfdd))
- Fix space before semicolons in comments ([`5f0dcc2`](https://codeberg.org/Elysium_Labs/eos/commit/5f0dcc2c936d97177acc17edbaab9be5d389e7e1))
- Merge pull request 'worktree-feat+eos-init' (#74) from worktree-feat+eos-init into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/74 ([`273234a`](https://codeberg.org/Elysium_Labs/eos/commit/273234a6bbe3f2036f35cf5e951c03c11959db9b))
- Merge pull request 'feat(install): suggest shell completion setup after install' (#71) from worktree-feat+issue-14-shell-completion-hint into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/71 ([`a3a02b6`](https://codeberg.org/Elysium_Labs/eos/commit/a3a02b6f79a6224343db204da630b020aa03804a))
- Merge pull request 'fix(api): route JSON output to stdout via OutOrStdout()' (#75) from worktree-feat+api-full-coverage into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/75 ([`a5727e9`](https://codeberg.org/Elysium_Labs/eos/commit/a5727e9b31d99a40bc021234b2ac30f06717736c))
- Merge pull request 'fix(schema): make runtime type and path independently optional' (#76) from fix/schema-runtime-required into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/76 ([`80efafc`](https://codeberg.org/Elysium_Labs/eos/commit/80efafcadc338054a299b87f2019005d04dbe976))
- Merge pull request 'test(daemon): add unit tests for daemon start --detach/-d flag' (#77) from feat/daemon-start-detach-tests into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/77 ([`0c8afd3`](https://codeberg.org/Elysium_Labs/eos/commit/0c8afd3bd0453f1ea8259bf7f58f9e135af38f25))
- Merge pull request 'docs(readme): add GitHub Actions deploy section' (#78) from docs/github-action into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/78 ([`acebc9e`](https://codeberg.org/Elysium_Labs/eos/commit/acebc9e1a7b50b0ead58715541807ed1530c8dae))
- Merge pull request 'docs: add CONTRIBUTING guide and PR template' (#79) from feat/contribution-docs into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/79 ([`eab72bb`](https://codeberg.org/Elysium_Labs/eos/commit/eab72bb0da4a5222882aafe607aa95602ecc45bb))
- Merge pull request 'feat(completion): add interactive shell completion installer' (#81) from feat/interactive-completion into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/81 ([`35f716c`](https://codeberg.org/Elysium_Labs/eos/commit/35f716c6e22cc9d1195b1b05bd480a978a79316b))
- Merge pull request 'feat/log-sink-plugin' (#80) from feat/log-sink-plugin into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/80 ([`a814d15`](https://codeberg.org/Elysium_Labs/eos/commit/a814d159aa63218f21b79a99b3175b02fb9379fa))


### Refactoring
- Replace skipManagerInit allowlist with lazy sync.Once init ([`74ad65c`](https://codeberg.org/Elysium_Labs/eos/commit/74ad65c7c113cfb3cd56560a83e9c14a2bde0f69))


### Testing
- Add unit tests for daemon start --detach/-d flag ([`7876de1`](https://codeberg.org/Elysium_Labs/eos/commit/7876de1243b1bdcffb1bcbae8688ad96291ce494))
- Add three-tier tests for sink plugin system ([`b9ac695`](https://codeberg.org/Elysium_Labs/eos/commit/b9ac6955d404714b5b100bda90e0424c297a9f79))
- Cover fixed lifecycle paths in sink process and ring buffer ([`22cba93`](https://codeberg.org/Elysium_Labs/eos/commit/22cba93dc915bfec1c6b17e8322395ea5855ef92))

## [0.0.11-rc.13] - 2026-07-04

### Features
- Add demo GIF to README ([`3aaa36d`](https://codeberg.org/Elysium_Labs/eos/commit/3aaa36df4e8518c0001125e542d4000d285b7d90))
- Harmonize log commands across service and daemon ([`f3be059`](https://codeberg.org/Elysium_Labs/eos/commit/f3be059e74133d929b8baeb71be72e69771f37e9))
- Add features section, env config, and service yaml examples ([`af31710`](https://codeberg.org/Elysium_Labs/eos/commit/af317108a688a33a4520294275c5c7cd8ce710ec))


### Miscellaneous
- Merge pull request 'feat(readme): add demo GIF to README' (#65) from feat/readme-demo-gif into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/65 ([`a2357d4`](https://codeberg.org/Elysium_Labs/eos/commit/a2357d4262a0140313ba91dde0151ac150cd3e4f))
- Merge pull request 'feat/test-coverage-improvements' (#66) from feat/test-coverage-improvements into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/66 ([`5550068`](https://codeberg.org/Elysium_Labs/eos/commit/55500681ee63216d025f7e7a8cd442faddc0a914))
- Merge pull request 'feat(logs): harmonize log commands across service and daemon' (#67) from feat/logs-combined-stream into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/67 ([`47cda0e`](https://codeberg.org/Elysium_Labs/eos/commit/47cda0e68c3339208bde063f06b95d8beb7fdd4d))
- Tweaks to readme ([`218a86e`](https://codeberg.org/Elysium_Labs/eos/commit/218a86ead20738e3273d7c5e40eeed1fd87215a7))


### Testing
- Add P1 unit tests and enforce 40% coverage threshold ([`66fc4ec`](https://codeberg.org/Elysium_Labs/eos/commit/66fc4ecd47f4e24ea2f8414b473cf923d3818bd8))
- Add P2 unit tests for scanStatusFieldBytes and checkUnknownProcess ([`81a594b`](https://codeberg.org/Elysium_Labs/eos/commit/81a594b2838496ffa79b3b34356343e4fba2aa67))
- Add P3 unit tests for lifecycle, stop, and runtime path ([`a0c83a2`](https://codeberg.org/Elysium_Labs/eos/commit/a0c83a284b0e390e97cbfe632a49a6640370d628))
- Add P4 IPC socket tests for DaemonManager ([`c4c4909`](https://codeberg.org/Elysium_Labs/eos/commit/c4c490965407dbe0fb1c84b3e5cca4e35564d1a6))
- Add P5 HTTP mock tests for system update flow ([`dc90152`](https://codeberg.org/Elysium_Labs/eos/commit/dc901522fe20073c4663344570f30bcdf30decbf))
- Fix correctness issues and remove dead code ([`338056f`](https://codeberg.org/Elysium_Labs/eos/commit/338056f2d9c794e8560627379b3bda348c3796f4))

## [0.0.11-rc.12] - 2026-07-03

### Bug Fixes
- Verify SHA256 checksum before installing binary ([`7baa4bf`](https://codeberg.org/Elysium_Labs/eos/commit/7baa4bf11ecf063021bdf12daa4e3c85c6feba6b))
- Preserve last RSS value during throttled mem sample ticks ([`fed938e`](https://codeberg.org/Elysium_Labs/eos/commit/fed938e86b3a357899ed4578aa6811e00b850169))


### Maintenance
- Replace Docker test targets with OrbStack equivalents ([`316f4b6`](https://codeberg.org/Elysium_Labs/eos/commit/316f4b681bd3f7e064f493070934874e84f25445))


### Miscellaneous
- Merge pull request 'fix(install): verify SHA256 checksum before installing binary' (#63) from fix/install-checksum-validation into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/63 ([`ab16463`](https://codeberg.org/Elysium_Labs/eos/commit/ab164630b9969e9da132736fa9801a4b21e09532))
- Merge pull request 'fix/status-memory-zero-on-throttle' (#64) from fix/status-memory-zero-on-throttle into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/64 ([`1c632e1`](https://codeberg.org/Elysium_Labs/eos/commit/1c632e17b93dad71cd55d3c54c334c9bd5c6dd89))

## [0.0.11-rc.11] - 2026-07-03

### Bug Fixes
- Fetch checksum from sha256sums.txt instead of API digest field ([`b2dc515`](https://codeberg.org/Elysium_Labs/eos/commit/b2dc5150e3a23c11a13c00c60ed331629c185e72))


### Miscellaneous
- Merge pull request 'fix(update): fetch checksum from sha256sums.txt instead of API digest field' (#62) from fix/checksum-validation-use-sha256sums into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/62 ([`3b08912`](https://codeberg.org/Elysium_Labs/eos/commit/3b08912e6364253eee3f1b0a068b3940ea571c47))

## [0.0.11-rc.10] - 2026-07-03

### Bug Fixes
- Connect directly to DB in systemd mode instead of erroring ([`e18f1c2`](https://codeberg.org/Elysium_Labs/eos/commit/e18f1c25c6bcac60ea7e7548be1d19852e4ff9c2))


### Features
- Migrate to slog with --verbose flag and e2e tests ([`af48a94`](https://codeberg.org/Elysium_Labs/eos/commit/af48a94c786d2863292b5b7cd40a695dd0681c43))
- Store service logs as JSON slog, render on eos logs ([`08205d0`](https://codeberg.org/Elysium_Labs/eos/commit/08205d0a1aa838fa231d672936ec861341fdee13))


### Miscellaneous
- Merge pull request 'worktree-feat+verbose-slog' (#61) from worktree-feat+verbose-slog into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/61 ([`6792426`](https://codeberg.org/Elysium_Labs/eos/commit/6792426c2de837124cef5f5a373b3aacda2716b6))

## [0.0.11-rc.9] - 2026-07-03

### Bug Fixes
- Reconcile orphan processes on daemon startup ([`663e837`](https://codeberg.org/Elysium_Labs/eos/commit/663e83740823ca1b34222d91f747500176386fbb))


### Features
- Prompt to stop running daemon before install ([`50aa56d`](https://codeberg.org/Elysium_Labs/eos/commit/50aa56d3e12cd5e4e8841f166e67782be555b3e2))
- Auto-detect system vs user systemd unit for startup/unstartup ([`b5043dc`](https://codeberg.org/Elysium_Labs/eos/commit/b5043dc047d0c7d0112ab4c22d7d61d412711644))
- Add DB seed helpers, bench suite, and golangci-lint worktree fix ([`4bc4236`](https://codeberg.org/Elysium_Labs/eos/commit/4bc4236f9c3ebae43f216633fab481cec4b99578))


### Miscellaneous
- Merge pull request 'feat: prompt to stop running daemon before install' (#57) from feat/install-stop-daemon into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/57 ([`f937a74`](https://codeberg.org/Elysium_Labs/eos/commit/f937a744f4075501f3643c8cab75d2e2ba0396a6))
- Merge pull request 'feat: auto-detect system vs user systemd unit for startup/unstartup' (#58) from feat/startup-multi-user-clarity into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/58 ([`37987ff`](https://codeberg.org/Elysium_Labs/eos/commit/37987ff3c114380032a081ba3dde8551dc363738))
- Merge pull request 'fix: reconcile orphan processes on daemon startup' (#59) from worktree-fix+orphan-reconciliation into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/59 ([`72ee22d`](https://codeberg.org/Elysium_Labs/eos/commit/72ee22d5a4ae33e786d80ed842f3580d4587f6a8))
- Merge pull request 'feat: add DB seed helpers, bench suite, and golangci-lint worktree fix' (#60) from worktree-test-data-seeding into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/60 ([`f8544db`](https://codeberg.org/Elysium_Labs/eos/commit/f8544db9eb7e6fc1d2036e080b124f71e9c0c1c7))

## [0.0.11-rc.8] - 2026-07-02

### Features
- Add Python runtime support ([`96749b5`](https://codeberg.org/Elysium_Labs/eos/commit/96749b5d0f845ad78c6ae9bdfc911a5381f4dcba))
- Add ~/.eos/config.yaml for daemon tunables ([`922bf0b`](https://codeberg.org/Elysium_Labs/eos/commit/922bf0bccff2f8ffdf153fc0e79ea6b84b7ec06a))
- Add env var overrides for all eos config tunables ([`bcf50fb`](https://codeberg.org/Elysium_Labs/eos/commit/bcf50fb8304bf545b7b966e8fe4cf0d1f67a128b))
- Add --watch / -w flag to status command ([`dade699`](https://codeberg.org/Elysium_Labs/eos/commit/dade6994c6e082a0209ecc2df3e5cd023ec7c2d8))
- Prompt user before removing a non-stopped service ([`c2d1f85`](https://codeberg.org/Elysium_Labs/eos/commit/c2d1f85d2118099baad86dd71a97d24f34689e90))


### Miscellaneous
- Merge pull request 'feat: add Python runtime support' (#52) from feat/issue-8-python-runtime into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/52 ([`ee58715`](https://codeberg.org/Elysium_Labs/eos/commit/ee5871572c32639ac7a3980c3494cddd01b2de93))
- Merge pull request 'feat: add ~/.eos/config.yaml for daemon tunables' (#53) from feat/issue-21-eos-config-yaml into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/53 ([`0e32317`](https://codeberg.org/Elysium_Labs/eos/commit/0e3231731d6f3bffcec68381d4d35e730899fee8))
- Merge pull request 'perf: reduce monitor allocations and add mem sample throttle' (#54) from feat/reduce-monitor-allocations into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/54 ([`d483655`](https://codeberg.org/Elysium_Labs/eos/commit/d483655e56e7505cbc7159ec121ceb7deda3c3f0))
- Merge pull request 'feat: add --watch / -w flag to status command' (#55) from feat/status-watch-flag into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/55 ([`1c2ffac`](https://codeberg.org/Elysium_Labs/eos/commit/1c2ffac742bc0aeb463e978c64de80139020020f))
- Merge pull request 'feat: prompt user before removing a non-stopped service' (#56) from feat/remove-state-guard into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/56 ([`89a63ad`](https://codeberg.org/Elysium_Labs/eos/commit/89a63adbc0803d7da11c64d69ba19d8710bdd9ba))


### Performance
- Reduce monitor allocations and add mem sample throttle ([`8ced47e`](https://codeberg.org/Elysium_Labs/eos/commit/8ced47e236a8a47e95634fe96a7ac92e9347f951))
- Eliminate 3 allocs per tick in isProcessAlive ([`600c7c8`](https://codeberg.org/Elysium_Labs/eos/commit/600c7c8a703fd5ea31f33f8e39925bb052f68855))
- Eliminate allocs in isProcessAlive and checkMemoryLinux ([`e0713ce`](https://codeberg.org/Elysium_Labs/eos/commit/e0713ce5692566fac748b0edb408bbbea5cc9ff2))
- Eliminate per-tick allocations in DB hot paths ([`0967408`](https://codeberg.org/Elysium_Labs/eos/commit/09674089d74f2729397628be9a7d60e7b5b0c5a4))

## [0.0.11-rc.7] - 2026-06-30

### Bug Fixes
- Memory limit restarts now respect maxRestartCount and backoff ([`3abf49c`](https://codeberg.org/Elysium_Labs/eos/commit/3abf49c2aec61217487eae6717f82f572e8fac5c))
- Eliminate Unknown transient state and guard nil StartedAt ([`365bd67`](https://codeberg.org/Elysium_Labs/eos/commit/365bd677317dd9f2ab1efc16e333eb890fef284d))
- Honour ctx cancellation in HealthMonitor, drop Stop() ([`030b20b`](https://codeberg.org/Elysium_Labs/eos/commit/030b20b67a5b1cd87bd327d67fa5a9aa4527ab91))
- Prevent root-owned ~/.eos when eos run as root ([`3e8229c`](https://codeberg.org/Elysium_Labs/eos/commit/3e8229ccb01cc0a57e9637aea9d50473e37a3dd9))
- Guard nil StartedAt in DetermineUptimeHuman and DetermineUptimeAPI ([`7dd85f4`](https://codeberg.org/Elysium_Labs/eos/commit/7dd85f47fc7bb1c0b23ba1358ab8c4cbd55fa303))
- Use hardcoded test config in newTestRootCmd instead of reading from disk ([`a123cc4`](https://codeberg.org/Elysium_Labs/eos/commit/a123cc4512d80a5aa06d51afa0e67242d2e5dd8b))


### CI/CD
- Drop nilaway — OOM-killed on free Codeberg runners ([`1979509`](https://codeberg.org/Elysium_Labs/eos/commit/19795091865b5246ecd7b03ed822e19712d06eb1))
- Remove empty nilcheck job ([`152da92`](https://codeberg.org/Elysium_Labs/eos/commit/152da92dc272067aead43f312704ffa5e5ba2ba3))


### Documentation
- Restructure README for clarity and user focus ([`d1b4eac`](https://codeberg.org/Elysium_Labs/eos/commit/d1b4eaced06d1e4a2b0c95860a0f6ad1ad2e1b55))


### Features
- Reset restart counter after stable uptime window ([`0b4dae5`](https://codeberg.org/Elysium_Labs/eos/commit/0b4dae59397a90171350f5618f5d5ca048adc044))
- Abstract exec calls behind Executor interface ([`5b6ac58`](https://codeberg.org/Elysium_Labs/eos/commit/5b6ac58f0987f2c27db7879b545e2faf475b6142))
- Add eos validate command ([`6b9ce4c`](https://codeberg.org/Elysium_Labs/eos/commit/6b9ce4c2b8a7a4b4752efdc10c1a22ae48d0d6e1))
- Collect all validation errors in validate command ([`4b18854`](https://codeberg.org/Elysium_Labs/eos/commit/4b18854b376b94979c730f6d064fa61f48c48e0f))


### Miscellaneous
- Merge pull request 'fix: memory limit restarts now respect maxRestartCount and backoff' (#43) from limit-restartcount-memory-restarts into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/43 ([`f9eab74`](https://codeberg.org/Elysium_Labs/eos/commit/f9eab7447281d3ff70c422874a30cb45e84696f7))
- Merge pull request 'feat: reset restart counter after stable uptime window' (#42) from worktree-feat+reset-restart-counter into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/42 ([`6cfd827`](https://codeberg.org/Elysium_Labs/eos/commit/6cfd827979eefdb571b060d19b75bb1fd4af76d6))
- Merge pull request 'feat: abstract exec calls behind Executor interface' (#44) from worktree-feat+executor-abstraction into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/44 ([`f0c2e4f`](https://codeberg.org/Elysium_Labs/eos/commit/f0c2e4f344f8230c6c80d2b91ab2c6a0528edcce))
- Merge pull request 'fix: eliminate Unknown transient state and guard nil StartedAt' (#45) from worktree-fix-unknown-state-health-monitor into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/45 ([`d47e6c7`](https://codeberg.org/Elysium_Labs/eos/commit/d47e6c74983f0e6616fe79658d5963a59806b4fb))
- Merge pull request 'docs: restructure README for clarity and user focus' (#46) from docs/readme-restructure into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/46 ([`cef7a0c`](https://codeberg.org/Elysium_Labs/eos/commit/cef7a0c9ea8800b790edd6e1eb8ad6b086304a65))
- Merge pull request 'fix: honour ctx cancellation in HealthMonitor, drop Stop()' (#48) from worktree-fix-health-monitor-ctx-cancellation into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/48 ([`e206249`](https://codeberg.org/Elysium_Labs/eos/commit/e2062497edf412c67e9fe957066abddf129813e3))
- Merge pull request 'fix: prevent root-owned ~/.eos when eos run as root' (#50) from fix/issue-49-root-owned-base-dir into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/50 ([`ffbebaf`](https://codeberg.org/Elysium_Labs/eos/commit/ffbebaf697383925bcc5877e13910051777f7dbc))
- Merge pull request 'feat: add eos validate command' (#51) from feat/issue-17-validate-command into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/51 ([`b6fbfb9`](https://codeberg.org/Elysium_Labs/eos/commit/b6fbfb92a4b2a7ac4927ec73f259c54ee0ed7fd6))


### Testing
- Fix TestNewSystemConfigHelper when run as root ([`83acf66`](https://codeberg.org/Elysium_Labs/eos/commit/83acf668a194519bf1f07f1759fd333a6ebc5d62))
- Set EOS_BASE_DIR in setupCmd to fix root-env test failures ([`1d6527d`](https://codeberg.org/Elysium_Labs/eos/commit/1d6527d543bd5563836e15e4635ce0367bbe4ef6))

## [0.0.11-rc.6] - 2026-06-29

### Bug Fixes
- Address security and reliability findings from CodeRabbit review ([`6b1c095`](https://codeberg.org/Elysium_Labs/eos/commit/6b1c0951bc2923273e5ee890d335ae6812620210))
- Address minor CodeRabbit findings across cmd and internal packages ([`97ec073`](https://codeberg.org/Elysium_Labs/eos/commit/97ec0736f49971403b87fb8821e3a1f442805d6e))
- Use upload-artifact@v3 in bench CI job ([`c30c64a`](https://codeberg.org/Elysium_Labs/eos/commit/c30c64a38cf9b719b68f55dcc70738258ac2a38d))


### CI/CD
- Use go-version-file and migrate issue templates to forgejo ([`1fa6578`](https://codeberg.org/Elysium_Labs/eos/commit/1fa6578bbdb037b567ec0f4e5965af90eb1922f7))


### Features
- Add memory/CPU profiling infra and fix daemon cmd testability ([`db2356e`](https://codeberg.org/Elysium_Labs/eos/commit/db2356e941dd26eecf89103556b9813207bf6cd5))


### Maintenance
- Expand linter rules, add ast-grep, and improve dev tooling ([`8991a4e`](https://codeberg.org/Elysium_Labs/eos/commit/8991a4e9c833fcb83cc514619dcd257769e7a1fa))


### Miscellaneous
- Merge pull request 'feat: adds boot persistence via systemd' (#39) from boot-persistence into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/39 ([`d36e687`](https://codeberg.org/Elysium_Labs/eos/commit/d36e68717f107561b1f6e857eab56401018b7220))
- Merge pull request 'boot-persistence' (#41) from boot-persistence into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/41 ([`a603c6e`](https://codeberg.org/Elysium_Labs/eos/commit/a603c6eed03a55f9cd95ac988d23ac53ab364740))


### Refactoring
- Restructure daemon config and thread ctx through Stop ([`4756fd9`](https://codeberg.org/Elysium_Labs/eos/commit/4756fd9f777f72570a143dd27be90cd885312003))


### Testing
- Add goroutine leak detection and fix test teardown ([`b849e25`](https://codeberg.org/Elysium_Labs/eos/commit/b849e257736ef7af43796e86a58b0783c33ffd05))
- Inject executor into startup/unstartup cmds and add tests ([`4ac2fe7`](https://codeberg.org/Elysium_Labs/eos/commit/4ac2fe73b6d0a5dccdbbb5be3e3656c20522ca69))
- Fix goroutine leaks in pipe-forwarding goroutines ([`65e74e5`](https://codeberg.org/Elysium_Labs/eos/commit/65e74e5d4d1084ff29a3a823e10e48eaf8704301))

## [0.0.11-rc.5] - 2026-05-03

### Features
- Adds env file parsing to building the environment ([`e37c1b6`](https://codeberg.org/Elysium_Labs/eos/commit/e37c1b6ffbc13fa86a10ca7ef8a04744b78d957f))
- Adds boot persistence via systemd ([`8386c40`](https://codeberg.org/Elysium_Labs/eos/commit/8386c408d1fcef2e30cb6eccee7f9ded4d7f947f))


### Maintenance
- Adds some test for environment handeling ([`bdad375`](https://codeberg.org/Elysium_Labs/eos/commit/bdad3759ed25589ba4f649cdeef18a9cee060413))


### Miscellaneous
- Merge pull request 'parse-env-file-service' (#38) from parse-env-file-service into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/38 ([`37d51b5`](https://codeberg.org/Elysium_Labs/eos/commit/37d51b551553b20c0b555b51e3b20b547f9441c8))

## [0.0.11-rc.4] - 2026-04-18

### Features
- Adds DSN to database driver for timeout and writer logs ([`b5cecc7`](https://codeberg.org/Elysium_Labs/eos/commit/b5cecc7c3ffb9f54ec75752ef00d9faf96454606))


### Improvements
- Updates dependencies to latest versions ([`6202309`](https://codeberg.org/Elysium_Labs/eos/commit/62023092ef06b8abba54e4ba35e3d6a7e8f72228))


### Maintenance
- Fixes broken install.sh and adjusts readme ([`6c8cdda`](https://codeberg.org/Elysium_Labs/eos/commit/6c8cddaf0f8c5abaa6d72fcb8ef5cf5930679b53))


### Miscellaneous
- Merge pull request 'chore: fixes broken install.sh and adjusts readme' (#27) from install-script-codeberg into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/27 ([`2aac0b0`](https://codeberg.org/Elysium_Labs/eos/commit/2aac0b0e9e6c2e9861d5aa6d7d35bb387b3c79b0))
- Enhances CLI text output coloring ([`79c1340`](https://codeberg.org/Elysium_Labs/eos/commit/79c1340da78632969fc84e0c1d9561eb0b57a81c))
- Merge pull request 'Enhances CLI text output coloring' (#35) from shell-runtime-support into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/35 ([`962d68a`](https://codeberg.org/Elysium_Labs/eos/commit/962d68ae856e2a4c8a78d637c59930972b1433fe))
- Merge pull request 'Updates dependencies to latest versions' (#36) from update-deps-18-apr-26 into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/36 ([`e4429f3`](https://codeberg.org/Elysium_Labs/eos/commit/e4429f398e544b70744b443f54aeeab232f8fecc))
- Merge pull request 'Adds DSN to database driver for timeout and writer logs' (#37) from db-retry-or-reconcile into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/37 ([`7309ef3`](https://codeberg.org/Elysium_Labs/eos/commit/7309ef3f9f08a66a7cfaba1d143ecc1e28b777f3))

## [0.0.11-rc.3] - 2026-04-10

### Bug Fixes
- Fixes invalid repo in install script ([`b5364a6`](https://codeberg.org/Elysium_Labs/eos/commit/b5364a6b5c823f4ffadcb81283a22e002058ca77))


### Maintenance
- Remaps all github urls to codeberg ([`e5c40bd`](https://codeberg.org/Elysium_Labs/eos/commit/e5c40bd91f6f3780ac3bcb16b5e9e06999edbcb7))


### Miscellaneous
- Merge pull request 'fix: update action in release pipeline to match forgejo capabilities' (#24) from release-pipeline into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/24 ([`d8899e9`](https://codeberg.org/Elysium_Labs/eos/commit/d8899e961f0798bb8804239eea9c129ce3058770))
- Merge pull request 'chore: remaps all github urls to codeberg' (#25) from release-pipeline into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/25 ([`e4f70a2`](https://codeberg.org/Elysium_Labs/eos/commit/e4f70a27f13ad1b033902c3489f028691e45cfc8))
- Merge pull request 'fix: fixes invalid repo in install script' (#26) from release-pipeline into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/26 ([`eb64b41`](https://codeberg.org/Elysium_Labs/eos/commit/eb64b4162a792582711f4be30d0e1afe2083656e))

## [0.0.11-rc.2] - 2026-04-06

### Bug Fixes
- Changes the runner in action to codeberg variants ([`ac515dc`](https://codeberg.org/Elysium_Labs/eos/commit/ac515dcb14a52a89970a50c681f0490512ac6f22))
- Various bug fixes ([`7afc6b7`](https://codeberg.org/Elysium_Labs/eos/commit/7afc6b7071c77ef118ca35103e52ed925af12c89))
- Various bug fixes ([`8925706`](https://codeberg.org/Elysium_Labs/eos/commit/8925706a2e2362adb25045eda672451f13131c72))
- Various bug fixes ([`376fee8`](https://codeberg.org/Elysium_Labs/eos/commit/376fee88f36934a5109474c12a6891212a0736b4))
- Invalid codeberg references ([`af6bd66`](https://codeberg.org/Elysium_Labs/eos/commit/af6bd6657508eeb4d4aee03b43923b8de90d465b))
- Update action in release pipeline to match forgejo capabilities ([`82a2590`](https://codeberg.org/Elysium_Labs/eos/commit/82a2590a3071ae84f871cac48a71ef711593af90))


### Features
- Api version of info command ([`da142eb`](https://codeberg.org/Elysium_Labs/eos/commit/da142eb447ab880b56a2b6ba3ad26e33111a202b))


### Improvements
- Update ISSUES.md ([`69296e1`](https://codeberg.org/Elysium_Labs/eos/commit/69296e1bed3363396431622d5350500f602bf947))


### Maintenance
- Moves project references from GitHub to Codeberg ([`71e7bb1`](https://codeberg.org/Elysium_Labs/eos/commit/71e7bb1a488e2fa0109de1c6e767c42c89993e37))
- Add manual push option to codeberg workflows ([`9995189`](https://codeberg.org/Elysium_Labs/eos/commit/9995189183da70228a8c8a415921607c6616f6ce))
- Adjusts version of golangci tool ([`549025d`](https://codeberg.org/Elysium_Labs/eos/commit/549025d72954e8479ede2ed40956686b44b7ac1c))
- Centralizes error messages, and handles sentinel errors in daemon communication ([`ee2da1f`](https://codeberg.org/Elysium_Labs/eos/commit/ee2da1f0ac0517643d7d25d4f2faf5d4c3ce573f))


### Miscellaneous
- Merge pull request 'codeberg-migration' (#1) from codeberg-migration into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/1 ([`09f558e`](https://codeberg.org/Elysium_Labs/eos/commit/09f558ea6086a3c6590021d23d33ec794ebe85e9))
- Merge pull request 'chore: centralizes error messages, and handles sentinel errors in daemon communication' (#22) from daemon-sentinel-errors into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/22 ([`de8b4c8`](https://codeberg.org/Elysium_Labs/eos/commit/de8b4c806760aafcda999457753edf720fd32cd1))
- Merge pull request 'feat: api version of info command' (#23) from info-api-command into main

Reviewed-on: https://codeberg.org/Elysium_Labs/eos/pulls/23 ([`cc2b3e7`](https://codeberg.org/Elysium_Labs/eos/commit/cc2b3e7b13c7737311e104f40c628d9e8c560eb7))

## [0.0.10] - 2026-04-05

### Bug Fixes
- Fix ldflags package path for version injection ([`48766dd`](https://codeberg.org/Elysium_Labs/eos/commit/48766dd221b1dcd7d6de51dd57246c075668e547))
- Fixes invalid test case expecting input ([`e3d23e9`](https://codeberg.org/Elysium_Labs/eos/commit/e3d23e9199f38b85ec9c4ebf15f9e6c4f2a3a08d))
- Fixes invalid tests cases for killing processes ([`c352457`](https://codeberg.org/Elysium_Labs/eos/commit/c352457bfc05ce200d3840c5dc78cdcfaed0be57))


### Improvements
- Improves daemon socket handeling ([`0d17e8d`](https://codeberg.org/Elysium_Labs/eos/commit/0d17e8d947271b7f6b5f5ecf120d3cc43b6d8b24))

## [0.0.11-rc.1] - 2026-04-04

### Features
- Adds uninstall command to system ([`cc1c0cb`](https://codeberg.org/Elysium_Labs/eos/commit/cc1c0cbe144da9983a37ecfb9356e2fe9947d63e))


### Improvements
- Improves CLI feedback ([`633677b`](https://codeberg.org/Elysium_Labs/eos/commit/633677bc0257b6f1d582b27b3ec71966eaa70913))

## [0.0.10-rc.9] - 2026-04-03

### Miscellaneous
- Changes module name to enable pkg.go.dev indexing ([`2eda9d6`](https://codeberg.org/Elysium_Labs/eos/commit/2eda9d6ecffb3e013878a3176be82f705efccfaf))

## [0.0.10-rc.8] - 2026-04-03

### Features
- Adds api versions of run and logs commands ([`22ed947`](https://codeberg.org/Elysium_Labs/eos/commit/22ed947ae3402e11b25d5257abe74a525aa39846))

## [0.0.10-rc.7] - 2026-04-03

### Improvements
- Updates the overall status table handeling - to allow stopped services ([`9e363d1`](https://codeberg.org/Elysium_Labs/eos/commit/9e363d196bfbaff5567b253be492c6de818bac4c))
- Updates readme with new run command ([`cdd4e86`](https://codeberg.org/Elysium_Labs/eos/commit/cdd4e8667eb776c3cb01fe53c5b1e7b6312633c2))
- Updates go version build pipelines ([`0d810c9`](https://codeberg.org/Elysium_Labs/eos/commit/0d810c9af7b56b80e89f376015b3347a74333e9a))


### Miscellaneous
- Memory check and limit setting available, improved CLI with examples, autocomplete and long desc ([`21a4b68`](https://codeberg.org/Elysium_Labs/eos/commit/21a4b6865e05d2ae74fd99b52f4bb5bd5134e2de))

## [0.0.10-rc.6] - 2026-03-15

### Miscellaneous
- Restores service log functionality with new pgid approach ([`325f599`](https://codeberg.org/Elysium_Labs/eos/commit/325f5999c4bb339b676ffadda362f051284d147c))

## [0.0.10-rc.5] - 2026-03-15

### Miscellaneous
- Creates new run command - which will replace the start and restart commands ([`f86f8b4`](https://codeberg.org/Elysium_Labs/eos/commit/f86f8b4d0973d05781881a749792800a53f0d9d9))

## [0.0.10-rc.4] - 2026-03-10

### Bug Fixes
- Fixes invalid file descriptor issue for system update ([`d34b061`](https://codeberg.org/Elysium_Labs/eos/commit/d34b0610aa2763a73889174071766c30fd7594bd))

## [0.0.10-rc.3] - 2026-03-09

### Miscellaneous
- Changes tracking processes from PID to PGID ([`aad9552`](https://codeberg.org/Elysium_Labs/eos/commit/aad955298f4e549f7dd59a4ea77d2efd4978bf0d))

## [0.0.10-rc.2] - 2026-03-07

### Improvements
- Improves update with precheck on backup folder access ([`906b7b6`](https://codeberg.org/Elysium_Labs/eos/commit/906b7b6a04da73d68677411dad7f850368a05aaa))
- Updates deps + updates linting in pipeline ([`efba6d9`](https://codeberg.org/Elysium_Labs/eos/commit/efba6d9f088385d69b7a9803d6c74348d99b006c))
- Updates linting in pipeline ([`88089e9`](https://codeberg.org/Elysium_Labs/eos/commit/88089e92d230dc84f8532d45df70e9efb557c176))

## [0.0.10-rc.1] - 2026-03-03

### Bug Fixes
- Fixes health_monitor tests failing in linux ([`3042389`](https://codeberg.org/Elysium_Labs/eos/commit/30423897eb7fdd311d1e47817e9fc1ac9f228da1))


### Miscellaneous
- Adjusts daemon protocol + rewrites process stopping ([`1b1843f`](https://codeberg.org/Elysium_Labs/eos/commit/1b1843f6f3492763d563d76e02a36fe1d43e5101))
- Adjusts test suite to address failing tests ([`362e1e0`](https://codeberg.org/Elysium_Labs/eos/commit/362e1e09709a8e0db427043d534a47313225c564))
- Removes obsolete claude branches ([`5a05161`](https://codeberg.org/Elysium_Labs/eos/commit/5a0516149af267882bd6edbc2d482019980c9535))
- Simplifies tests to enable cross OS test results ([`3bbfe8c`](https://codeberg.org/Elysium_Labs/eos/commit/3bbfe8c1d64a01c9fb66a1be066a63cbd691fd67))

## [0.0.9-rc.2] - 2026-02-25

### Bug Fixes
- Fixes invalid test for system update ([`96e8603`](https://codeberg.org/Elysium_Labs/eos/commit/96e860356a0f73d0d4850a7ffd46b0f4b6f6de09))

## [0.0.9-rc.1] - 2026-02-25

### Features
- Adds support for pre-releases in building and consuming ([`be27d61`](https://codeberg.org/Elysium_Labs/eos/commit/be27d61780722474c7d2f5c3bc6419255463b7b8))

## [0.0.8] - 2026-02-25

### Features
- Adds request for daemon restart after binary update + adds more info to daemon info command ([`e71b3bc`](https://codeberg.org/Elysium_Labs/eos/commit/e71b3bc182faf5b3a67ed0296daab6925a2dc750))

## [0.0.7] - 2026-02-25

### Improvements
- Improves log output for services ([`4db7ba1`](https://codeberg.org/Elysium_Labs/eos/commit/4db7ba185d50f9139d1f97fdb4ab7d2f7fa19199))

## [0.0.6] - 2026-02-25

### Improvements
- Improves log output for services and daemon ([`91c302e`](https://codeberg.org/Elysium_Labs/eos/commit/91c302e4d0d9235571ec1d0f951af87396e486cc))

## [0.0.5] - 2026-02-25

### Improvements
- Improves system update process to adhere to linux file rules ([`b8a52ea`](https://codeberg.org/Elysium_Labs/eos/commit/b8a52eae481d8bb100b7c6c530484d7c8fdbc20b))

## [0.0.4] - 2026-02-25

### Miscellaneous
- Enhances cli experience with improved messages ([`4e6e4c7`](https://codeberg.org/Elysium_Labs/eos/commit/4e6e4c7680d417b176e46c06000b656684c0cfbb))

## [0.0.3] - 2026-02-24

### Improvements
- Updates release pipeline ([`aac8bb2`](https://codeberg.org/Elysium_Labs/eos/commit/aac8bb2bf640dda9f60425f20a09ceb57a7d4509))
- Updates buildinfo handeling + allows for local binary installation via install.sh ([`f3ee51c`](https://codeberg.org/Elysium_Labs/eos/commit/f3ee51c001d3ffc8554f0afe084edcf3478e5cbc))

## [0.0.2] - 2026-02-24

### Bug Fixes
- Fixes mismatch in requirements for ci pipeline ([`f159cdf`](https://codeberg.org/Elysium_Labs/eos/commit/f159cdf00b39ec7b52902081ae654f6ffbc94f9b))
- Fixes timezone mismatch in different environment for tests ([`7718788`](https://codeberg.org/Elysium_Labs/eos/commit/771878833cc9e76ef8bb063dafbf39efa80e21a3))


### Improvements
- Update README.md ([`f54c5c9`](https://codeberg.org/Elysium_Labs/eos/commit/f54c5c976c41130f6d00442eacf86c18e0428765))
- Updates install shell commands + adds Makefile with useful shorthands ([`20eb879`](https://codeberg.org/Elysium_Labs/eos/commit/20eb879bf4d0f487f9f21f08d8bf7785e9c06a8f))


### Miscellaneous
- Clarify config management in README

Removed mention of database sync in the README. ([`2eef37c`](https://codeberg.org/Elysium_Labs/eos/commit/2eef37ceeb9f553412a9c0ab262841ec25d94c46))
- Tweaks to Github Actions + improved test coverage + changes based on linting rules ([`6dbf0e4`](https://codeberg.org/Elysium_Labs/eos/commit/6dbf0e4ea8953b2ff6164c21c4af291c606042d9))
- Disables port checking and related tests + adds improved handeling for error cases ([`714affb`](https://codeberg.org/Elysium_Labs/eos/commit/714affb4d74e89f7b5dc185702134670f67db81c))
- Adjusts ci pipeline by splitting linting into own and removing the codecoverage upload ([`e7db799`](https://codeberg.org/Elysium_Labs/eos/commit/e7db79958aca1061bf8881dc6d99eff3d7954283))
- Pins golangci version in pipeline to match local + adds additional fixes to precommit ([`b47c6b5`](https://codeberg.org/Elysium_Labs/eos/commit/b47c6b58f6bdf81db418f566e4cd9f1872b28bbc))


### Refactoring
- Refactors codebase to new linting standards ([`a7e88f1`](https://codeberg.org/Elysium_Labs/eos/commit/a7e88f154c97d10f0220ec5e10cbe801973d6416))

## [0.0.1] - 2026-01-25

### Bug Fixes
- Fixes invalid sqlite package reference ([`cb3edb1`](https://codeberg.org/Elysium_Labs/eos/commit/cb3edb13e8b6850e70954d478798e1b56cb6d0af))


### Features
- Adds github deploy pipeline to the project ([`1168f3a`](https://codeberg.org/Elysium_Labs/eos/commit/1168f3aa6b3871874fbd0bab84d15aab6c6e140c))
- Adds Apache 2.0 license and updates readme ([`eb2053b`](https://codeberg.org/Elysium_Labs/eos/commit/eb2053b19ee25d75369758054bbbc7fa611d5f85))
- Adds database migrations and migration tests ([`838b15c`](https://codeberg.org/Elysium_Labs/eos/commit/838b15cb1fd3dec4671243552cf44e4c0f7c2787))


### Improvements
- Improves cli output with new lines ([`25e4716`](https://codeberg.org/Elysium_Labs/eos/commit/25e47164c40d736ac37cea7cb573d8542479f2b9))


### Miscellaneous
- Initial commit ([`0078430`](https://codeberg.org/Elysium_Labs/eos/commit/00784306bdb84d99bf0a9a96ef74c309f964be9f))
- Updating main package references ([`8bf0d19`](https://codeberg.org/Elysium_Labs/eos/commit/8bf0d197081c4f481a667c4834fb7ed8be92860a))

