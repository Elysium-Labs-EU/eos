# Changelog

All notable changes to eos are documented here.

## [0.0.13] - 2026-08-13

### Bug Fixes
- Stop racing daemon recovery test against a fixed timeout (#273) ([`ba51c1c`](https://github.com/Elysium-Labs-EU/eos/commit/ba51c1cd81c35cba9cc0599b415bd607b9f49216))
- Scope manifest since flag, note empty systemd journal (#275) ([`b465577`](https://github.com/Elysium-Labs-EU/eos/commit/b465577dfbb73a78d39df92c3239518c2a06f581))
- Isolate the pre-commit lint cache too (#278) ([`7274ebb`](https://github.com/Elysium-Labs-EU/eos/commit/7274ebba04119a4c02e655ac4fdc46b1d1af1dc1))


### Documentation
- Drop the shared ADR index, surface status via adr-find (#276) ([`ffa1e41`](https://github.com/Elysium-Labs-EU/eos/commit/ffa1e4105d051279c3d1a7b12e9444709f1f251d))


### Maintenance
- Drop the ADR index row instruction from worker guidance (#274) ([`6807767`](https://github.com/Elysium-Labs-EU/eos/commit/68077673767046d9695571a66082c604d069a0bc))
- Give each worktree its own golangci-lint cache (#277) ([`f33836f`](https://github.com/Elysium-Labs-EU/eos/commit/f33836f0988a41e0e9647bed204343733bd8f85e))

## [0.0.13-rc.9] - 2026-08-10

### Bug Fixes
- Warn on stale binary for standalone daemon info (#253) ([`e36d20c`](https://github.com/Elysium-Labs-EU/eos/commit/e36d20cf82976e86d3375e206e60226ea80c3332))
- Bound crash-reason line to pgid, prefer error lines (#254) ([`002f930`](https://github.com/Elysium-Labs-EU/eos/commit/002f9303d638f028b3126e8d9fd32863333a8ade))
- Do not treat a clean exit-0 as a startup crash (#259) ([`dbb5a41`](https://github.com/Elysium-Labs-EU/eos/commit/dbb5a4168530b2fd5c36fe88ab1de955cb258367))
- Local mode blocks and supervises instead of returning (#268) ([`ea16772`](https://github.com/Elysium-Labs-EU/eos/commit/ea16772cfb52f748e8c7ee3fa0539816ea9e0770))
- Preflight the command in the launch environment (#271) ([`ccda73e`](https://github.com/Elysium-Labs-EU/eos/commit/ccda73edd651502d0a0ec4622b8d2707e45595c3))


### Documentation
- Adopt ADRs as the permanent design record (#249) ([`8427ae7`](https://github.com/Elysium-Labs-EU/eos/commit/8427ae7e815931aa9b76642b3aa5d14cce58a4c3))


### Features
- Negotiate sink plugin protocol version on READY (#258) ([`9f91e32`](https://github.com/Elysium-Labs-EU/eos/commit/9f91e326dabcf6897fa67cfaf94b3bd1c144fd97))
- Detect sustained restart-failure loops (#265) ([`d4aac5e`](https://github.com/Elysium-Labs-EU/eos/commit/d4aac5e5203492c2b9c0c8deaf5fce51d971a188))
- Collect the daemon and service environment (#266) ([`14b63d5`](https://github.com/Elysium-Labs-EU/eos/commit/14b63d52078ed4c95cd2a0adcd24f03d63757a7f))


### Maintenance
- Stop telling workers to do something they are denied (#252) ([`2f40064`](https://github.com/Elysium-Labs-EU/eos/commit/2f4006429771949f26ef4e011c068a8d6693c9d4))
- Tell the reviewer to check the merge base before calling a revert (#256) ([`d33c361`](https://github.com/Elysium-Labs-EU/eos/commit/d33c361f661d26d72bf94582ae720e79f118cdf5))
- Allow the merge check and raise the diff ceiling (#267) ([`d0a4c64`](https://github.com/Elysium-Labs-EU/eos/commit/d0a4c64623076d2b4ad20f3ca3d4ab26e9ee0330))
- Nest workers in one workspace, warn on the two shared-state traps (#269) ([`6b07c1e`](https://github.com/Elysium-Labs-EU/eos/commit/6b07c1e30e885ce9a0aa07efa24687c21ae340fe))
- Add the launch-environment preflight to the v0.0.13-rc.9 changelog (#272) ([`a1a4b16`](https://github.com/Elysium-Labs-EU/eos/commit/a1a4b166a8d17083c0eaba8c8c0ab3ca57104f68))


### Miscellaneous
- Make adr-find usable from a git worktree (#250) ([`a09725d`](https://github.com/Elysium-Labs-EU/eos/commit/a09725de0c06d9b4ab1a43f9cb1109cadf2db7bf))
- Ignore the local code-intelligence index repo-wide (#251) ([`42802be`](https://github.com/Elysium-Labs-EU/eos/commit/42802be680e52ba4dd67d1b4307d037f4c418f22))

## [0.0.13-rc.8] - 2026-08-09

### Bug Fixes
- Install openssl when missing instead of refusing (#245) ([`55e8148`](https://github.com/Elysium-Labs-EU/eos/commit/55e8148eb6c18fb6d3749f8ebf3b5be84d8b9b48))
- Resolve systemd daemon version, warn on stale binary (#244) ([`ae7331c`](https://github.com/Elysium-Labs-EU/eos/commit/ae7331caa233b4c2dfae3bcadef3950e1629b33a))
- Surface leaked process groups from older history rows (#246) ([`22ea4bc`](https://github.com/Elysium-Labs-EU/eos/commit/22ea4bc3c88843fb616af9f0372a24968f1e3d67))


### Maintenance
- Encode this repo's real testing gates in the worker brief (#247) ([`2c73f0e`](https://github.com/Elysium-Labs-EU/eos/commit/2c73f0e97ecbc400c55e8e91c238750411661a2e))

## [0.0.13-rc.7] - 2026-08-09

### Bug Fixes
- Route CLI through daemon socket for supervised installs (#236) ([`fe36ce2`](https://github.com/Elysium-Labs-EU/eos/commit/fe36ce25bf924606299649c458d0bac3d0629153))
- Treat Unknown process-history rows as live, not terminal (#239) ([`7088c0f`](https://github.com/Elysium-Labs-EU/eos/commit/7088c0f64342ea8a1c4d53a1c441358039fd3ef0))
- Escalate to SIGKILL when a restart grace period expires (#240) ([`87e9d5d`](https://github.com/Elysium-Labs-EU/eos/commit/87e9d5dc78dd75861f47561fe8e1b9de04f7a141))
- Defer disable persist until stop succeeds, name --force (#238) ([`4668d41`](https://github.com/Elysium-Labs-EU/eos/commit/4668d41dfca28abc2ae743bb52ca2b0c028ab60c))


### Features
- Add test-supervision-orb target, fold typos into ci (#241) ([`f620a80`](https://github.com/Elysium-Labs-EU/eos/commit/f620a808d0400752670f011cf182421691799713))

## [0.0.13-rc.6] - 2026-08-08

### Bug Fixes
- Harden curl/npm install flags in test-fixtures-orb.sh (#229) ([`60eef10`](https://github.com/Elysium-Labs-EU/eos/commit/60eef10e7657fb3da5f1b94dfc5bdf0b573a58f4))


### Features
- Add JSON schema for config.yaml (#228) ([`30baf47`](https://github.com/Elysium-Labs-EU/eos/commit/30baf47ad3473619ff0cab2262f09654a54c2758))


### Maintenance
- Suppress SonarCloud go:S2077 for database.go dynamic SQL (#227) ([`3d0495c`](https://github.com/Elysium-Labs-EU/eos/commit/3d0495c43cc7d5f897093bbc0081540aee45587f))

## [0.0.13-rc.5] - 2026-08-08

### Bug Fixes
- Journalctl fallback for daemon log in diagnose bundle (#218) ([`15e53a1`](https://github.com/Elysium-Labs-EU/eos/commit/15e53a14f1b9f4426607783d8cc3ad78b85a134f))


### Documentation
- Add SECURITY.md for private vulnerability reporting (#221) ([`6596abb`](https://github.com/Elysium-Labs-EU/eos/commit/6596abb79fe26907fa101be2a6bb743dd547168b))


### Features
- Print next-step instructions after writing bundle (#223) ([`f16c474`](https://github.com/Elysium-Labs-EU/eos/commit/f16c474ab893dd91b925d00c0540aaae505954e9))

## [0.0.13-rc.4] - 2026-08-08

### Bug Fixes
- Dont miss live child when leader is already reaped (#219) ([`f2cd91a`](https://github.com/Elysium-Labs-EU/eos/commit/f2cd91a7f5865e31ee4ccc5c974360b107a0c361))

## [0.0.13-rc.3] - 2026-08-08

### Bug Fixes
- Gitleaks allowlist missing lowercase MIIBogIBAAJ fixture regex (#209) ([`b1c3416`](https://github.com/Elysium-Labs-EU/eos/commit/b1c34168f511b9ffb28aa7905245e25735de6fcb))
- Delegate HealthMonitor.isProcessAlive to procutil.IsAlive (#212) ([`389238e`](https://github.com/Elysium-Labs-EU/eos/commit/389238efe73f06cc4991983fbce29d219a15adab))
- Gate startup Running transition on port reachability (#214) ([`6c74f43`](https://github.com/Elysium-Labs-EU/eos/commit/6c74f43b8a0cfb70ad008fa5a5974dbde7a5d765))


### Features
- Add eos diagnose privacy-safe diagnostic bundle command (#205) ([`da43f81`](https://github.com/Elysium-Labs-EU/eos/commit/da43f81b94d69ad0713f0e28a08bdf87539a8707))
- Add eos config show/init/validate for config.yaml discoverability (#213) ([`20d71f3`](https://github.com/Elysium-Labs-EU/eos/commit/20d71f38a1fdff87b832891346948b49b820e3eb))


### Miscellaneous
- Commit CI dev-tools cache-skip fix, fixture-test automation, argus config (#210) ([`efb6aa4`](https://github.com/Elysium-Labs-EU/eos/commit/efb6aa463a065ef5cae6c31a7cd132731b35eaed))


### Refactoring
- Centralize eos command name strings in internal/cmdnames (#208) ([`8bc687f`](https://github.com/Elysium-Labs-EU/eos/commit/8bc687feecd527c73c239293ad4abe27bb3e87fa))


### Testing
- Commit real-app fixture corpus for test-fixtures-orb.sh (#211) ([`7756fd4`](https://github.com/Elysium-Labs-EU/eos/commit/7756fd4fad800615309e8e1dddafebb86863b2e3))

## [0.0.13-rc.2] - 2026-08-07

### Bug Fixes
- Heal systemd user bus on daemon Start, tolerate slow restarts (#206) ([`34e2b02`](https://github.com/Elysium-Labs-EU/eos/commit/34e2b02c1459f1bf0efa5a76a93c365da56072bb))


### Maintenance
- Bump crate-ci/typos from 1.48.0 to 1.49.0 (#187) ([`a57aba4`](https://github.com/Elysium-Labs-EU/eos/commit/a57aba400048cc4ee062c100afb1351dfef55734))
- Bump the go-dependencies group across 1 directory with 9 updates (#188) ([`7152e83`](https://github.com/Elysium-Labs-EU/eos/commit/7152e83642401c46c5b79df68f8ca6db24920b5b))


### Miscellaneous
- Removes arrow from TUI (#178)

Co-authored-by: sgnilreutr <sgnilreutr@noreply.codeberg.org> ([`6c0447c`](https://github.com/Elysium-Labs-EU/eos/commit/6c0447c28dadfa8837fc204df4dc7784bf5f430e))

## [0.0.13-rc.1] - 2026-08-07

### Bug Fixes
- Persist per-service stop/run state across daemon restarts (#177) ([`e568f98`](https://github.com/Elysium-Labs-EU/eos/commit/e568f98f2404d48bb957e986c94110c2646024eb))
- Correct daemon.wait line number in go-crap-gate OS_INTEGRATION_EXEMPT (#201) ([`a453b33`](https://github.com/Elysium-Labs-EU/eos/commit/a453b3380b8e2eb6f03ba31cb2dec65c23d1a974))


### CI/CD
- Cache dev tool binaries, stop serializing Verify Build after CI (#202) ([`5f42852`](https://github.com/Elysium-Labs-EU/eos/commit/5f428527ff08da957bb8a4b589c0d13e9e3982f2))


### Features
- Explain and prompt to enable linger on user-unit startup (#174) ([`621ce37`](https://github.com/Elysium-Labs-EU/eos/commit/621ce3750b5969d9ff8e53c5b7dc01c4daaa2f94))


### Miscellaneous
- Triage 17 bare TODO comments flagged as debt (#164) ([`dc05495`](https://github.com/Elysium-Labs-EU/eos/commit/dc0549502a2ce881cf488c718136d6f914715d27))
- Extract duplicated string literals into constants (58 findings) (#168) ([`bc0e0d1`](https://github.com/Elysium-Labs-EU/eos/commit/bc0e0d1a45b89c802d660e55665adb1f5dbc1cee))


### Refactoring
- Go idiom cleanup (ctx threading, param grouping) (#170) ([`492cf95`](https://github.com/Elysium-Labs-EU/eos/commit/492cf95fbc525d9252069343c306a913a0b3b71a))
- Extract helpers to reduce cognitive complexity (#171) ([`bc2ebfe`](https://github.com/Elysium-Labs-EU/eos/commit/bc2ebfedfd9e4ff4ab5b206fd9a39fc31ded5510))


### Testing
- Exercise stop force-quit decline path reliably (#176) ([`2ef7ae5`](https://github.com/Elysium-Labs-EU/eos/commit/2ef7ae5495cf0e69e7b5bdbcd9f6537020bc0a12))
- Cover manager error paths for remove/status/update via mockMgr (#175) ([`84fda40`](https://github.com/Elysium-Labs-EU/eos/commit/84fda40bf32ef6798245c06c1bc6be429a515175))

## [0.0.12] - 2026-08-06

### Bug Fixes
- Add depends_on and max_wait to service.schema.json (#144) ([`78d7824`](https://github.com/Elysium-Labs-EU/eos/commit/78d78243bbd41c8fb11922fdd9a29589d02d3ff5))
- Process-group liveness check, remove fixed-wait flakes (#148) ([`861f8cf`](https://github.com/Elysium-Labs-EU/eos/commit/861f8cf69fb1ebf06bce5badbfbf48e98ca6eec2))
- Resolve sonar shell lint findings in install.sh and scripts/ (#163) ([`62aabad`](https://github.com/Elysium-Labs-EU/eos/commit/62aabadebe425b79997ba58a3252820cfec234f2))


### CI/CD
- Add SonarQube Cloud scanning workflow (#146) ([`38c21e8`](https://github.com/Elysium-Labs-EU/eos/commit/38c21e86b867253899feb22bf173fddc1b8d6ce7))
- Add typos, mod-verify, diff-size, no-interface{}, and API-diff gates (#147) ([`235af7c`](https://github.com/Elysium-Labs-EU/eos/commit/235af7c8c6af42cdf2814d1e8fdda2317b501032))


### Maintenance
- Bump modernc.org/sqlite in the go-dependencies group (#142) ([`be4ed70`](https://github.com/Elysium-Labs-EU/eos/commit/be4ed7006e357a6f08ca5ffde5cc5e70499f265e))


### Miscellaneous
- Support depends_on in service.yaml for start ordering (#140)

* feat: surface depends_on wait as a distinct eos status state

Closes #136

* Support depends_on in service.yaml for start ordering

Closes #136

* test: cover dependency cycle and self-dependency fail-loud paths

---------

Co-authored-by: sgnilreutr <sgnilreutr@noreply.codeberg.org> ([`f98ae69`](https://github.com/Elysium-Labs-EU/eos/commit/f98ae6956b80f28a8c0a89efc389f2f5280d05db))
- Allowlist SQL columns for dynamic query safety (S2077) (#165) ([`c396916`](https://github.com/Elysium-Labs-EU/eos/commit/c39691615af5c2d99aedd9f7639740439f0d9c0a))
- Resolve PATH commands via LookPath, cap install perms (#166) ([`cd4cae8`](https://github.com/Elysium-Labs-EU/eos/commit/cd4cae8ab2e033bcb43be1b01fc109484cfddb41))
- Predictable/public-writable temp file path in cmd/root.go (#169) ([`c118977`](https://github.com/Elysium-Labs-EU/eos/commit/c118977ecf0bed7288535974c1561d350df9efd6))
- Harden CI workflows (SHA pin, least-privilege perms) (#167) ([`f8022ed`](https://github.com/Elysium-Labs-EU/eos/commit/f8022ed983ad7ae3c8be0ae2ad05883702324058))

## [0.0.12-rc.10] - 2026-07-30

### Bug Fixes
- Honor --pre when selecting latest release (#115) ([`71dac28`](https://github.com/Elysium-Labs-EU/eos/commit/71dac2828bb388e0f97f2b689f25ffe4f68163d3))


### Features
- Gate service start on depends_on readiness with max_wait ceiling (#118) ([`5ffab16`](https://github.com/Elysium-Labs-EU/eos/commit/5ffab16cbd5c20b7b20328d6e4a90321da6013b2))
- Add zero-downtime reload command with SO_REUSEPORT cutover (#119) ([`d4bb729`](https://github.com/Elysium-Labs-EU/eos/commit/d4bb72986415a81da3a222bc62d263989156a479))
- Enforce release signature verification (#123) ([`20df4c1`](https://github.com/Elysium-Labs-EU/eos/commit/20df4c161b667091a9b75b448fae8bb5e726fb16))
- Authorize daemon control socket via peer credentials (#125) ([`f59a59f`](https://github.com/Elysium-Labs-EU/eos/commit/f59a59fe13fbfba276591a73dfd82fbb761fe3a8))
- Add gitleaks secret scanning to the CI gate (#127) ([`0628b72`](https://github.com/Elysium-Labs-EU/eos/commit/0628b72468681f63e9d318f3f82cac8774b9ce38))
- Add govulncheck reachability scan to CI gate (#130) ([`588c9f3`](https://github.com/Elysium-Labs-EU/eos/commit/588c9f315ff5281837387d9af1629272d0e168a4))
- Add eos snapshot save/restore commands (#139) ([`4f81ad3`](https://github.com/Elysium-Labs-EU/eos/commit/4f81ad335a2c90c577f175f1793ada078065b806))


### Maintenance
- Enable Dependabot for gomod and github-actions (#129) ([`13444bb`](https://github.com/Elysium-Labs-EU/eos/commit/13444bb79dcdd1e447f94442706f3562d07cbeda))
- Bump actions/upload-artifact from 6 to 7 (#133) ([`888ce95`](https://github.com/Elysium-Labs-EU/eos/commit/888ce95b9a13a341a589a788a6dedd571dc50e1c))
- Bump actions/download-artifact from 7 to 8 (#132) ([`a79a545`](https://github.com/Elysium-Labs-EU/eos/commit/a79a545a24aa004db2d8160196cdd14cd1c12522))
- Bump softprops/action-gh-release from 2 to 3 (#131) ([`eb4ad3f`](https://github.com/Elysium-Labs-EU/eos/commit/eb4ad3ffb74f48f3cca293aacb012bd42596a81a))
- Bump the go-dependencies group with 3 updates (#135) ([`60f2898`](https://github.com/Elysium-Labs-EU/eos/commit/60f28981eed89c4feaeb901a6183be4d5a35785f))
- Bump actions/setup-go from 6 to 7 (#134) ([`1d55ccd`](https://github.com/Elysium-Labs-EU/eos/commit/1d55ccdde4ad845a78c4db2490324eb59761c1b3))
- Align release lint version with make ci (#141) ([`c19ce42`](https://github.com/Elysium-Labs-EU/eos/commit/c19ce42a9618fa264ccde86e3b7da018902ca98b))


### Miscellaneous
- EOS_BASE_DIR override validation (Medium, redacted) (#126) ([`5a828a0`](https://github.com/Elysium-Labs-EU/eos/commit/5a828a02ebe8d6a5d8143f2afbdfdc63a6118411))

## [0.0.12-rc.9] - 2026-07-27

### Bug Fixes
- Eos-fix-issue-107 (#107) (#111) ([`d8870cc`](https://github.com/Elysium-Labs-EU/eos/commit/d8870cc57ab6eacb5f68be4d59b2989efd38ae4e))
- Eos-fix-issue-108 (#108) (#110) ([`1d96e00`](https://github.com/Elysium-Labs-EU/eos/commit/1d96e001f6f2296c06474fb5897344d8534d256c))
- Eos-fix-issue-109 (#109) (#112) ([`d30b60a`](https://github.com/Elysium-Labs-EU/eos/commit/d30b60aa8279dd2bd179137f6f301667dcd84260))
- Eos-fix-issue-106 (#106) (#113) ([`225bacd`](https://github.com/Elysium-Labs-EU/eos/commit/225bacd99f3a1b9c403399a07a7b52a30f1d81f4))

## [0.0.12-rc.8] - 2026-07-25

### Bug Fixes
- Eos-fix-issue-102 (#102) (#103) ([`c47c2ac`](https://github.com/Elysium-Labs-EU/eos/commit/c47c2ac5e670a376d7bb60f7c06965a404cadf19))
- Eos-fix-issue-102 (#102) (#105) ([`605c45a`](https://github.com/Elysium-Labs-EU/eos/commit/605c45a7ba6f50bbc3070b1b96145c15badda8d0))

## [0.0.12-rc.7] - 2026-07-25

### Bug Fixes
- Eos-fix-issue-93 (#93) (#95) ([`0b2137e`](https://github.com/Elysium-Labs-EU/eos/commit/0b2137e045bf1a9596bba87ce729f46f990f3305))
- Eos-fix-issue-94 (#94) (#96) ([`68ef908`](https://github.com/Elysium-Labs-EU/eos/commit/68ef90874a94e24b1e308ae1efd1767659409118))
- Eos-fix-issue-97 (#97) (#99) ([`b95c13e`](https://github.com/Elysium-Labs-EU/eos/commit/b95c13e3f34667f87d8044b9de72c58a2ac57d4e))
- Eos-fix-issue-98 (#98) (#100) ([`b8f39ee`](https://github.com/Elysium-Labs-EU/eos/commit/b8f39ee4ed9e03065a44e12b11269c31dfeb5cb3))

## [0.0.12-rc.6] - 2026-07-25

### Bug Fixes
- Eos-fix-issue-65 (#65) (#89) ([`1e896cd`](https://github.com/Elysium-Labs-EU/eos/commit/1e896cd09ad65b7418c4f6dcf3b9c6dfcaf71772))
- Eos-fix-issue-66 (#66) (#84) ([`464adb2`](https://github.com/Elysium-Labs-EU/eos/commit/464adb288368557e3d0fd9e190f22d0a373987eb))
- Eos-fix-issue-76 (#76) (#83) ([`57f26d5`](https://github.com/Elysium-Labs-EU/eos/commit/57f26d541d3b02e578cbfe4ef66acb6a05d6ca24))
- Eos-fix-issue-68 (#68) (#88) ([`0677a32`](https://github.com/Elysium-Labs-EU/eos/commit/0677a322f31ed40a05bd52ab99e3ab7620ebc9e3))
- Eos-fix-issue-69 (#69) (#82) ([`e469860`](https://github.com/Elysium-Labs-EU/eos/commit/e469860187abf2448cce499a2a59937ad49742ac))
- Eos-fix-issue-73 (#73) (#90) ([`3e904f7`](https://github.com/Elysium-Labs-EU/eos/commit/3e904f7259ef364bcb95e14dc0e8cde9dcf28808))
- Eos-fix-issue-75 (#75) (#87) ([`1b86c81`](https://github.com/Elysium-Labs-EU/eos/commit/1b86c814a07e860fdfd3f075fb47f6a21261f410))
- Eos-fix-issue-78 (#78) (#86) ([`b7f74d2`](https://github.com/Elysium-Labs-EU/eos/commit/b7f74d2e678f2b5c1dac9688f06d585e078b5d6c))
- Eos-fix-issue-67 (#67) (#85) ([`89ba99f`](https://github.com/Elysium-Labs-EU/eos/commit/89ba99f9d5b6148d91f81efc6794a92b43cfac5e))
- Eos-fix-issue-91 (#91) (#92) ([`0e6fd42`](https://github.com/Elysium-Labs-EU/eos/commit/0e6fd42f1fa82804f6021b8de97bae08e2c7d411))


### CI/CD
- Trigger on merge_group for merge queue support (#81) ([`17b4710`](https://github.com/Elysium-Labs-EU/eos/commit/17b4710be2cf4664b14701c9b25b7f85bb207c8d))


### Miscellaneous
- Recover ~5 days of merged work lost in the main force-push (2026-07-19 to 07-23) (#80)

* Merge pull request #5 from Elysium-Labs-EU/chore/migrate-to-github

chore: complete Codeberg to GitHub migration
(cherry picked from commit 409a53aba4464ecb9982806345507ab60df59f43)

* Merge pull request #7 from Elysium-Labs-EU/docs/add-logo

Add eos logo to README

(cherry picked from commit 291dc8fcabf418b73afd4471377485ad1b7a10b1)

* Merge pull request #15 from Elysium-Labs-EU/fix/schema-ahead-recovery-11

fix(database): detect schema-version-ahead DB and cap systemd crash-loop (#11)

(cherry picked from commit f65353c1ac0c4fc153e07d9adeb87c6c1b2a9eba)

* Merge pull request #16 from Elysium-Labs-EU/fix/base-dir-db-ownership-14

fix(database): chown state.db to base dir owner under sudo (#14)

(cherry picked from commit 2d981732fbbcd1c88bdab9d6eb64724698adeea4)

* Merge pull request #17 from Elysium-Labs-EU/fix/openrc-daemon-control-13

fix(daemon): delegate daemon lifecycle to OpenRC (issue #13)

(cherry picked from commit 032ac21656598076c3360a49d8ea3071a0a3840f)

* Merge pull request #18 from Elysium-Labs-EU/fix/socket-path-len-9

fix(daemon): actionable error when unix socket path exceeds AF_UNIX limit (macOS)

(cherry picked from commit 955605e41e3b6fe0aa34d8d110f0e55115055087)

* Merge pull request #19 from Elysium-Labs-EU/fix/case-insensitive-logname-10

fix: reject case-only-different service names to stop shared-log leak (#10)
(cherry picked from commit 2fe4059b07a8f42c199ac99e592c923df0b8c873)

* Merge pull request #20 from Elysium-Labs-EU/fix/basedir-daemon-liveness-12

fix: scope systemd daemon-liveness to the active EOS_BASE_DIR (#12)
(cherry picked from commit b96bd780580e3266e63aefc7d9aca57a2f1bca26)

* Merge pull request #21 from Elysium-Labs-EU/fix/concurrent-run-race-1

fix(manager): serialize per-service start/stop to end concurrent-run race

(cherry picked from commit ffc8529fbc8065f039f0fcca7b7cbe89e07302c9)

* Merge pull request #23 from Elysium-Labs-EU/feat/release-signing

feat(security): sign releases and verify signatures (F-001/F-002)

(cherry picked from commit e016a5bce87a0b3017becb79934a2a2370b5336b)

* Merge pull request #22 from Elysium-Labs-EU/fix/reconcile-orphans-pgid-reuse-2

fix(daemon): don't SIGKILL recycled PGIDs in reconcileOrphans

(cherry picked from commit da4c844aa641cb56b40ab6bd8feb853695ef0d00)

* Merge pull request #24 from Elysium-Labs-EU/feat/ci-full-gate

ci: run make ci in full, no hand-copied step subset
(cherry picked from commit b8bce8e7c8c07b3d6e522ea5ff9627b60f73e85e)

* Merge pull request #25 from Elysium-Labs-EU/chore/rotate-release-signing-key

chore: rotate release-signing public key
(cherry picked from commit 1a837bd171c77059df542ac0305137569c62cf51)

* feat(daemon): report running binary version in systemd daemon info (#26)

printSystemdDaemonDetails only printed static hint commands, never the
actual version of the binary backing the running process — so after an
update lands but before the unit restarts, there was no way to tell
from 'eos daemon info' which version was actually running.

Resolve it by following /proc/<pid>/exe from the unit's MainPID and
querying that exact binary's --version, mirroring how StaleBinary
already detects the same binary-vs-running drift for standalone
daemons.

(cherry picked from commit 77bb53d3057bccba34fb37a619ddb228cc601a0e)

* fix: fix-issue-8 (#8) (#28)

Closes #8

Co-authored-by: sgnilreutr <sgnilreutr@noreply.codeberg.org>
(cherry picked from commit 3a214c42bc06078a211a80602fac4a5558c7c187)

* Merge pull request #33 from Elysium-Labs-EU/fix-issue-32

fix: fix-issue-32 (#32)
(cherry picked from commit 7a0cd645ca2bbf2a84dda81ef96205a7720f3f44)

* Merge pull request #35 from Elysium-Labs-EU/fix-issue-34

fix: fix-issue-34 (#34)
(cherry picked from commit 1f0fadd74775637646dae1527e17707b8e9fb2a3)

* feat(system): darwin release support + non-interactive system startup/unstartup (#31)

* feat(system): darwin release support + non-interactive system startup/unstartup

Two related pieces, both surfaced by building a Go tool (eos-plugins'
tunnel-setup) that shells out to eos non-interactively:

eos already has full launchd support (PR #118) but the release pipeline
only ever built linux-amd64/linux-arm64, and install.sh had no OS
detection at all (hardcoded "eos-linux-${arch}" everywhere) — so there
was no way to actually get eos onto a Mac via the normal install path.

- release.yml: add darwin/amd64+arm64 to the build matrix, built on
  macos-latest (needed for the codesign step, which only runs on macOS)
  with an ad-hoc `codesign --force -s -`, mirroring argus's existing
  pattern.
- install.sh: add detect_os() alongside the existing detect_arch(),
  replace hardcoded "linux" with ${os} in the download URL and checksum
  lookup, add strip_quarantine() for the Gatekeeper com.apple.quarantine
  xattr, and add resign_darwin_binary() — an unconditional re-codesign
  after the binary lands in its final installed location.

  The re-sign step matters beyond quarantine-stripping: reproduced a
  real macOS kernel-level SIGKILL ("Code Signature Invalid" /
  CODESIGNING "Invalid Page") on an ad-hoc-signed Go binary after
  overwriting an existing file in place at the same path — a real,
  cited failure class (golang/go#42684, golang/go#63997, golang/go#64351,
  and more — see argus#66), not just quarantine. Validated live: full
  `sudo bash install.sh -y --local dist/eos-darwin-arm64` install on
  this machine, confirmed the installed binary runs cleanly.
- Makefile: release-local now builds all 4 platform binaries; added
  test-install-darwin mirroring the existing test-install-orb pattern.

Both commands prompted interactively with no way to skip, blocking any
script/tool that invokes them non-interactively (found the hard way:
stdin unconnected → immediate EOF → command fails even on a completely
fresh, no-conflict install). Threaded flagYes through all six
startup/unstartup implementations (systemd, launchd, OpenRC), matching
the existing flagYes || helpers.PromptConfirm(...) pattern already used
by `eos add`/`eos remove`/`eos uninstall`.

* fix: integration test build break from flagYes param, bump 3 CVE'd deps

startupCmd/unstartupCmd/startupCmdLaunchd/unstartupCmdLaunchd gained a
flagYes bool param but the integration-tagged test call sites (excluded
from the default build, so CI/Verify Build stayed green while Integration
Test failed) weren't updated. Pass false to preserve the existing
stdin-driven confirm/decline flow these tests rely on.

Also bumps golang.org/x/net (GO-2026-5942), golang.org/x/text
(GO-2026-5970), and google.golang.org/grpc (GHSA-hrxh-6v49-42gf) past
their flagged versions to clear the OSV scan.

---------

Co-authored-by: sgnilreutr <sgnilreutr@noreply.codeberg.org>
(cherry picked from commit 138b68199b929b776a5486328a26a635f1564063)

* fix: fix-issue-30 (#30) (#37)

* fix: fix-issue-30 (#30)

Closes #30

* fix: fix-issue-30 (#30)

Closes #30

---------

Co-authored-by: sgnilreutr <sgnilreutr@noreply.codeberg.org>
(cherry picked from commit 5b63f8f5327b0516f7ec86b412af04473fed1cb2)

* fix: fix-issue-27 (#27) (#36)

* fix: fix-issue-27 (#27)

Closes #27

* fix(daemon): show running version in standalone daemon info

Issue #27 wants 'eos daemon info' to show which eos version the
daemon is running. PR #36 wired version-drift detection into
'eos system version' but never surfaced it in 'daemon info' for the
standalone supervisor (systemd's info output already did this via
PR #26). Reuse resolveStandaloneDaemonVersion to print a
'running version:' line, matching systemd's placement, and only
when the daemon is actually running; a failed version query is
silently swallowed so it never breaks the rest of the output.

---------

Co-authored-by: sgnilreutr <sgnilreutr@noreply.codeberg.org>
(cherry picked from commit c6271b70eba5a1a95dda4542ed68e3c2615bf29f)

* fix(install): sync stale signing key, fix exit-code trap bug, update next-steps hint (#40)

* fix(install): sync stale signing key, fix exit-code trap bug, update next-steps hint

Fixes #39

* fix(install): tolerate whitespace after colon in fetch_json_field

GitHub's releases API returns pretty-printed JSON in some cases
(space after the colon), which the previous no-space-only regex
never matched -- silently breaking auto-detection of the latest
version and forcing EOS_VERSION as a workaround.

Fixes #39

---------

Co-authored-by: sgnilreutr <sgnilreutr@noreply.codeberg.org>
(cherry picked from commit 4b5b27ba308027ce14ec3693cbcfc8f732911d77)

* fix(database): reduce alignDataFileOwnership complexity and add real coverage

Recovered PR #16 introduced alignDataFileOwnership/NewDB before PR #24 (which
first enabled the go-crap gate in CI) existed, so neither was ever actually
gate-checked. Landing #16 today trips the now-active gate: root-only chown
logic can't gain coverage from the non-sudo test run go-crap's Makefile
target uses, so the fix is complexity reduction plus covering every branch
that doesn't actually require root (stat resolution, missing-file tolerance,
same-owner chown) rather than chasing unreachable coverage.

* fix(ci): install golangci-lint via go install, not their broken install.sh

golangci-lint's official install.sh's hash_sha256_verify() does a plain
grep "${BASENAME}" against checksums.txt to find the expected hash. Since
recent releases also publish a NAME.tar.gz.sbom.json file (whose name is a
superstring of the tarball's), the grep matches both lines and the
resulting multi-line $want can never equal the single-line $got --
checksum verification fails deterministically on every install, on every
platform, for any release that ships sbom files (confirmed reproducing
against the real v2.12.2 checksums.txt). go install uses Go's own module
checksum verification instead, sidestepping the bug entirely, and matches
how every other dev tool in this target is already installed.

---------

Co-authored-by: sgnilreutr <sgnilreutr@noreply.codeberg.org> ([`fab8fca`](https://github.com/Elysium-Labs-EU/eos/commit/fab8fca9472fd0a6187fd7548015bb31018bd38f))

## [0.0.12-rc.4] - 2026-07-24

### Bug Fixes
- Default daemon start to background, add --foreground opt-in ([`7764858`](https://github.com/Elysium-Labs-EU/eos/commit/7764858802d68604f094cbb20cfc3302f6c69ba8))
- Store timestamps without monotonic clock suffix ([`25a3818`](https://github.com/Elysium-Labs-EU/eos/commit/25a38184f10c5d112ae0bbd448a76bb69c6b12dc))
- Warn on run/start when the eos daemon is down (#142) ([`a59ccd5`](https://github.com/Elysium-Labs-EU/eos/commit/a59ccd5a2d9ce7e45fc4e3cf818365a2500e4b2b))
- Back off and surface permission errors on unwritable logs (#145) ([`528cc7e`](https://github.com/Elysium-Labs-EU/eos/commit/528cc7e73e43297d4efa1802da02bcc4bfd7b890))
- Heartbeat process_history.updated_at every health tick (#143) ([`f3aec7b`](https://github.com/Elysium-Labs-EU/eos/commit/f3aec7b1cc0ffd02709e5fad15a9905388835ea8))
- Fix-issue-156 (#156) ([`c403004`](https://github.com/Elysium-Labs-EU/eos/commit/c403004cd9b2b92add896de3b310c182e3e66289))
- Fix-issue-156 (#156) ([`8c7f44f`](https://github.com/Elysium-Labs-EU/eos/commit/8c7f44fa0ec4f31d44d550bd0b6333d25757e182))
- Fix-issue-158-retry (#158) ([`ef5f9f7`](https://github.com/Elysium-Labs-EU/eos/commit/ef5f9f753072dd7cc1b3eca47781490c58bf251a))
- Validate service name to prevent log path traversal ([`7afc232`](https://github.com/Elysium-Labs-EU/eos/commit/7afc2324e033fc0475d6bd6b49b008896d984df9))
- Eos-fix-issue-54 (#54) (#56) ([`5e07737`](https://github.com/Elysium-Labs-EU/eos/commit/5e07737c0a6de897d2210d9d63709008c934692d))
- Eos-fix-issue-46 (#46) (#52) ([`29a6966`](https://github.com/Elysium-Labs-EU/eos/commit/29a6966843f89b959936ec8e03c1441298559d79))
- Eos-fix-issue-57 (#57) (#58) ([`fcb87f0`](https://github.com/Elysium-Labs-EU/eos/commit/fcb87f016f2a400ba6977bb5994b6bc88b98f49d))
- Eos-fix-issue-55 (#55) (#59) ([`34a8ed2`](https://github.com/Elysium-Labs-EU/eos/commit/34a8ed2f06bdc8cceea39568f22ffb4410f906fd))
- Eos-fix-issue-45 (#45) (#51) ([`048f59e`](https://github.com/Elysium-Labs-EU/eos/commit/048f59ebf5a21de40cb17882fdca4e845a9f8292))
- Eos-fix-issue-47 (#47) (#53) ([`3dc93d5`](https://github.com/Elysium-Labs-EU/eos/commit/3dc93d5b289f6ec09d54981a0b261ede37388ebc))
- Eos-fix-issue-41 (#41) (#60) ([`cd8d671`](https://github.com/Elysium-Labs-EU/eos/commit/cd8d671c16cc7845b98a5adaf7a87331e71e1388))
- Eos-fix-issue-43-recover (#43) (#61) ([`7a8dba9`](https://github.com/Elysium-Labs-EU/eos/commit/7a8dba9ff0f9e1bd5ca13281b4f68285b68e2f39))
- Eos-fix-issue-62 (#62) (#63) ([`cb9690c`](https://github.com/Elysium-Labs-EU/eos/commit/cb9690c0499fd68555eb957dc0dcf94df35b42ad))
- Migrate module path and all release/download references from Codeberg to GitHub (#64) ([`9f69298`](https://github.com/Elysium-Labs-EU/eos/commit/9f6929872423f6c6b8895cc01a92f5c4dbed6f03))


### CI/CD
- Drop dead permissions field from Forgejo workflows ([`2ac2188`](https://github.com/Elysium-Labs-EU/eos/commit/2ac2188018f96b25142c96c4bce1e2bfa0c667b0))
- Tune OSV scan (deps-only PRs + weekly cron, prebuilt scanner) ([`db6e5ec`](https://github.com/Elysium-Labs-EU/eos/commit/db6e5ecfa9b728dccea9001b389f6b529f6550f5))


### Documentation
- State Codeberg is canonical repo in README ([`f1671b3`](https://github.com/Elysium-Labs-EU/eos/commit/f1671b327ab707503b6fa9879abb90e06f9d0162))


### Features
- Track per-service CPU usage in status ([`631ab12`](https://github.com/Elysium-Labs-EU/eos/commit/631ab12e1cbb71ac7edb2e85005270d208761cf4))
- Warn when daemon is down on read commands (#141) ([`960a7f3`](https://github.com/Elysium-Labs-EU/eos/commit/960a7f3b1b7ebae161a657852986a1cebed0fb54))
- Flag stale process_history rows by updated_at age ([`2053070`](https://github.com/Elysium-Labs-EU/eos/commit/2053070be0ae369007a6ccac11cb4a5b03dc3082))
- Per-service RSS memory tracking ([`6834d14`](https://github.com/Elysium-Labs-EU/eos/commit/6834d144b8988a9cfa1377e985fc38d66fb734e9))
- Daemon-native OpenTelemetry metrics and traces ([`41e438f`](https://github.com/Elysium-Labs-EU/eos/commit/41e438fe452abb0dc80c1c072597d610c10b1662))


### Maintenance
- Reduce newStandaloneDaemon complexity and drop scratch dir ([`1d3e84b`](https://github.com/Elysium-Labs-EU/eos/commit/1d3e84bfbbbbc7a6e035925f4a9bcfd471b48bd5))


### Miscellaneous
- Stage release download in a private mktemp dir ([`4c439ba`](https://github.com/Elysium-Labs-EU/eos/commit/4c439ba042eb8d9c92d666592ed04f52c1d519c5))


### Testing
- Real-process degraded-mode coverage for #142/#143/#145 (#146) ([`e1d3ca5`](https://github.com/Elysium-Labs-EU/eos/commit/e1d3ca56e68a9e4cfce9827ae63e61a3cc2fc8b9))

## [0.0.12-rc.3] - 2026-07-17

### Bug Fixes
- Thread resolved Identity through system unstartup paths too ([`5f0425e`](https://github.com/Elysium-Labs-EU/eos/commit/5f0425e3327863a4c2056c66d5eff82723b2a76d))
- Show resolved user/base dir when no services registered ([`a389d25`](https://github.com/Elysium-Labs-EU/eos/commit/a389d25c71a656f843cd127a99cecd512d099d27))
- Thread Identity through status.go's daemonIdentity after GetBaseDir signature change ([`fd93a43`](https://github.com/Elysium-Labs-EU/eos/commit/fd93a4360e77935dd1575f0849fe7ae7211c7ed4))
- Compare start time alongside PGID in liveness checks ([`b175418`](https://github.com/Elysium-Labs-EU/eos/commit/b1754187d26cc6d17d91643dc24acf010cd978f6))
- Make service stop/start robust to instant-exit and PGID reuse ([`f7cca3c`](https://github.com/Elysium-Labs-EU/eos/commit/f7cca3c90b9295d0556419e57a86c6a11e2936a2))


### CI/CD
- Re-run ([`9a3dc30`](https://github.com/Elysium-Labs-EU/eos/commit/9a3dc30cdd5ae19e4fe52c2d0993ae6d81d76abd))
- Re-run ([`69776b8`](https://github.com/Elysium-Labs-EU/eos/commit/69776b82e6837123dec3987bbe5688817a23468d))
- Re-run 2 ([`31d4994`](https://github.com/Elysium-Labs-EU/eos/commit/31d499449a6af883f89e861fa03423913b413a92))
- Tolerate flaky bench artifact upload ([`0ca52fe`](https://github.com/Elysium-Labs-EU/eos/commit/0ca52fe60b86f9bb91541d394961f82153adba63))
- Make go-crap gate change-scoped to PR churn ([`fd1ce2b`](https://github.com/Elysium-Labs-EU/eos/commit/fd1ce2ba3a820a4333c8d7bf634adbe093b509f9))
- Run Go jobs in container with named-volume caches ([`179491b`](https://github.com/Elysium-Labs-EU/eos/commit/179491b17a7ab60d9f1cf28f97400ba0d249a260))


### Features
- Warn on self-detaching service commands (setsid/nohup/disown) ([`cbf5a2e`](https://github.com/Elysium-Labs-EU/eos/commit/cbf5a2e0d9385562860449cbd5c8a4a7048f1487))
- Add eos env command for per-service environment variables ([`b057322`](https://github.com/Elysium-Labs-EU/eos/commit/b0573226bf759f215d5e8d01d3155316403305bd))
- Named log sink registry in daemon config ([`b1e1776`](https://github.com/Elysium-Labs-EU/eos/commit/b1e1776e7c1430e4e54f075abfbd4d045ea3738d))
- Cron_restart field in service.yaml (#10) ([`c18a3c6`](https://github.com/Elysium-Labs-EU/eos/commit/c18a3c692497fddb02c2f7560af59dc07b28d51f))


### Maintenance
- Remove dead GitHub-Actions-only permissions block from workflows ([`68eb167`](https://github.com/Elysium-Labs-EU/eos/commit/68eb1672827d3d69d55b8da94f2fc301a0129200))
- Add go-crap hard gate for change-risk analysis ([`6cd1282`](https://github.com/Elysium-Labs-EU/eos/commit/6cd12820f4e15830043aa9f9a54ee550b945ea2f))
- Exclude cobra command builders from the gate ([`38578ca`](https://github.com/Elysium-Labs-EU/eos/commit/38578ca5d3f57c66abbc7669d38b855580c04e46))


### Miscellaneous
- Make identity/privilege resolution a compiler-enforced type ([`94d202b`](https://github.com/Elysium-Labs-EU/eos/commit/94d202b68146d809d4071c5347d73778b4b41ceb))
- Merge remote-tracking branch 'origin/main' into eos-10-cron-restart

# Conflicts:
#	internal/types/types.go ([`926aaa0`](https://github.com/Elysium-Labs-EU/eos/commit/926aaa0f3943c51ec0cb52b5f0a4eaac10c0057a))
- Merge remote-tracking branch 'origin/main' into chore/strip-dead-permissions ([`d0fc74d`](https://github.com/Elysium-Labs-EU/eos/commit/d0fc74d99191d30a2fd5816ad579451b40005ec6))
- Merge remote-tracking branch 'origin/main' into chore/go-crap-gate ([`06d110e`](https://github.com/Elysium-Labs-EU/eos/commit/06d110e06e0984e5bb639e33eed9d79de9c0efb1))
- Bump go directive to 1.26.5 ([`f89299b`](https://github.com/Elysium-Labs-EU/eos/commit/f89299bfbcd2dc96a233f06d4256d2934c64edc8))


### Refactoring
- Split executeRequest into per-method handlers ([`e6fd0da`](https://github.com/Elysium-Labs-EU/eos/commit/e6fd0da5a8d18ef8405f518cbe516665fbe14e95))
- Cut CRAP in service start/stop/restart lifecycle ([`472a9b8`](https://github.com/Elysium-Labs-EU/eos/commit/472a9b8fc3b91ebfa4dbfcdf969927cab6c70494))
- Cut CRAP in system update/startup command logic ([`05b92c4`](https://github.com/Elysium-Labs-EU/eos/commit/05b92c48fc9c95d5b037308d023889e0e70ab738))
- Cut CRAP in reaper, discovery, and fork paths ([`04c1ac8`](https://github.com/Elysium-Labs-EU/eos/commit/04c1ac8f0e7023d47aa3343faab2377daad5201d))
- Cut CRAP in scope resolution, memory scan, and seed helpers ([`c89c2e4`](https://github.com/Elysium-Labs-EU/eos/commit/c89c2e46f79dc5d938abb89bc9f2505790631d7b))


### Testing
- Add margin below CRAP-30 for checkMemoryLinux and newManager ([`fbb0521`](https://github.com/Elysium-Labs-EU/eos/commit/fbb0521e9b6c9e09c4b34654f449dccf6569ee9a))

## [0.0.12-rc.2] - 2026-07-16

### Bug Fixes
- Atomic binary swap in install.sh ([`2327b4d`](https://github.com/Elysium-Labs-EU/eos/commit/2327b4dc53f123ca3cfbd681c7c9cbdaf2b5f082))
- Skip merge commits and changelog-bump commits from changelog ([`d638ca1`](https://github.com/Elysium-Labs-EU/eos/commit/d638ca19e6db67ee190690bd510ad0ee5f912504))
- Point missing-plugin error at eos-plugins repo ([`39e4c7a`](https://github.com/Elysium-Labs-EU/eos/commit/39e4c7a1de20d507acc8c776c179adbe010b41a0))
- Use full GitHub URL for osv-scanner-action, not mirrored on Forgejo ([`e83ef1e`](https://github.com/Elysium-Labs-EU/eos/commit/e83ef1eefdabe86af6e947dcc460573aff44ed6c))


### CI/CD
- Add OSV scanner workflow for PRs to main ([`6f3aada`](https://github.com/Elysium-Labs-EU/eos/commit/6f3aadad2a2abbd839ce10a8339ed4172ca9e2b4))
- Run osv-scanner CLI directly instead of GitHub Action wrapper ([`3ed2637`](https://github.com/Elysium-Labs-EU/eos/commit/3ed2637beb4ac57382dc39c59a54d0e692a67217))


### Features
- Add eos daemon info --all for cross-user visibility ([`7bfacf6`](https://github.com/Elysium-Labs-EU/eos/commit/7bfacf6acf658c9209aa3561af400e756438b858))
- Add top-level --version/-v flag ([`ee6b48b`](https://github.com/Elysium-Labs-EU/eos/commit/ee6b48b00cf7c6c859e469b0fd1e380242c72487))
- Add OpenRC boot persistence for non-systemd distros ([`bd04e25`](https://github.com/Elysium-Labs-EU/eos/commit/bd04e25de4a01a5e52a7e85ad4bb8c122a2ffc12))
- Launchd support for boot persistence on macOS ([`65d5437`](https://github.com/Elysium-Labs-EU/eos/commit/65d5437666c42d876125bccd26bee8520724d942))

## [0.0.12-rc.1] - 2026-07-14

### Bug Fixes
- Generate real changelog notes for Codeberg releases ([`a328845`](https://github.com/Elysium-Labs-EU/eos/commit/a328845ce06e74ac8e04838a95fa6b9b56adee84))
- Describe eos as a service supervisor, not "modern" ([`5d06f5b`](https://github.com/Elysium-Labs-EU/eos/commit/5d06f5b4280cbae66271b11a3e233fdfea9403bf))
- Detect host arch for git-cliff install ([`6e8685e`](https://github.com/Elysium-Labs-EU/eos/commit/6e8685edd3cc4f51aa2302c4af2beb29cf0bec63))
- Verify OS process liveness before trusting DB state (#96) ([`251f74d`](https://github.com/Elysium-Labs-EU/eos/commit/251f74d54c67dbe196553a4990834266237a13d6))
- Use zombie-aware liveness check for Linux CI parity ([`2f10565`](https://github.com/Elysium-Labs-EU/eos/commit/2f10565380acd917d06a2c0594f5506050f80911))


### Documentation
- Point make ci-full at Linux-parity tests before push ([`9fc426d`](https://github.com/Elysium-Labs-EU/eos/commit/9fc426dcc8bc34de5339126516d50a6625edc622))


### Features
- Fall back to full help on bare invocation, show version ([`18d9471`](https://github.com/Elysium-Labs-EU/eos/commit/18d94714cae7d6f0ab4913ce8a8942d42db92563))
- **breaking** Remove deprecated start and restart commands ([`be4caba`](https://github.com/Elysium-Labs-EU/eos/commit/be4caba490dab0ebc53aca9fac066a231a82e26e))

## [0.0.11-rc.25] - 2026-07-13

### Bug Fixes
- Render starting status distinct from unknown ([`261a837`](https://github.com/Elysium-Labs-EU/eos/commit/261a8372168e7c1533710801940b3ee1c5da3726))
- Return proper exit codes from daemon/system commands ([`ae29a09`](https://github.com/Elysium-Labs-EU/eos/commit/ae29a0931d37800db08dcf9977cc6864d1d01eda))
- Recover from stale Starting process history entries ([`7950fa6`](https://github.com/Elysium-Labs-EU/eos/commit/7950fa67e9dd29439b9c4acd5cc0a7f93bb3b6a3))
- Stop GetBaseDir honoring SUDO_USER when not running as root ([`d1df1af`](https://github.com/Elysium-Labs-EU/eos/commit/d1df1afbd775b7cd3f1143c4b80f6f1acefaa215))


### Documentation
- Add rule on ambient lookups as hidden dependencies ([`34baf62`](https://github.com/Elysium-Labs-EU/eos/commit/34baf62ff55ad3aa4750bd6762f746fc348cce73))


### Maintenance
- Default to help target when make runs with no args ([`7c7fcb0`](https://github.com/Elysium-Labs-EU/eos/commit/7c7fcb0e99c830a713e81ac0cdf577009b9d0845))
- Remove completed TEST_COVERAGE_TODO.md ([`cadd2d9`](https://github.com/Elysium-Labs-EU/eos/commit/cadd2d93cdf99507c56ef78a09c5ab22769f22b7))


### Miscellaneous
- Merge remote-tracking branch 'origin/main' into worktree-test-coverage-review ([`345783b`](https://github.com/Elysium-Labs-EU/eos/commit/345783b1a0c1e57f5b1679d6d1da5286b7b5d54c))
- Drop arrow glyphs from CLI hint text ([`e4be32d`](https://github.com/Elysium-Labs-EU/eos/commit/e4be32dbc8eb01da810dc0badd6bf0ae3591e150))

## [0.0.11-rc.24] - 2026-07-10

### Bug Fixes
- Correct log rotation, IPC arg, and error-wrap bugs found in review ([`f30e751`](https://github.com/Elysium-Labs-EU/eos/commit/f30e75105a8cf8b49fdfd177a74e676f0e1f07f9))
- Add panic isolation and port-reachability check to health monitor ([`d612680`](https://github.com/Elysium-Labs-EU/eos/commit/d612680f2964653b95533abb60ebb7480a1f1aa2))
- Correct dead sentinel check and mistested command in api info ([`2684ada`](https://github.com/Elysium-Labs-EU/eos/commit/2684ada70a0642c990c0ee1c0321973c9ac2efd0))
- Prevent panic when api run is called with no args or -f ([`560e529`](https://github.com/Elysium-Labs-EU/eos/commit/560e529331688851fd746b0b94692c40aa92676d))
- Wire ValidateServiceConfig into add/run/update, matching api validate ([`494ded8`](https://github.com/Elysium-Labs-EU/eos/commit/494ded8638e6810071becc9ab0ab906216e79eae))
- Wire ValidateServiceConfig into interactive add, matching api add ([`0c9a226`](https://github.com/Elysium-Labs-EU/eos/commit/0c9a226863b76ee43fb4e4c36cdf5152f9693a84))
- Make interactive add return a real error on failure ([`8af316a`](https://github.com/Elysium-Labs-EU/eos/commit/8af316ae3ce1609445c3a8fe5bc434d3df4f8703))
- Convert remaining interactive commands from Run to RunE ([`a0fa671`](https://github.com/Elysium-Labs-EU/eos/commit/a0fa671f6aa9c9296cd28ed5b863762c90ac773e))
- Route status table/watch-frame output through cmd, not raw stdout ([`f12135d`](https://github.com/Elysium-Labs-EU/eos/commit/f12135d8cc2b89382963e0a731b4868f8ffc9ec5))
- Correct combined-log merge order and surface tail failures ([`b33bcc2`](https://github.com/Elysium-Labs-EU/eos/commit/b33bcc2aee3475c800ce9c2ace8623b94ed0bd21))
- Honor status in DetermineProcessMemoryInMbAPI, stop PromptConfirm dropping answers ([`8245299`](https://github.com/Elysium-Labs-EU/eos/commit/824529991e07dc5b70b0d268c15c4722ee4e46c1))
- Preserve correct HOME for privilege-dropped detach child ([`c463237`](https://github.com/Elysium-Labs-EU/eos/commit/c46323724ecd18bbd337c0b46c409b53f442925f))
- Stop startup/unstartup hardcoding the real "eos" unit ([`ffb4dfa`](https://github.com/Elysium-Labs-EU/eos/commit/ffb4dfa495119d2b910b1703b413f76f9944b97d))
- Flush plugin stdin after each record ([`451f1a2`](https://github.com/Elysium-Labs-EU/eos/commit/451f1a288a25ea45ce0d1bae54fca431d70b84d7))


### Testing
- Clean up stale comments and misleading assertion messages ([`e3bd399`](https://github.com/Elysium-Labs-EU/eos/commit/e3bd399137a70c6faa1a058f8ba81968b6525f1a))
- Clarify regression comments with git history ([`561eef8`](https://github.com/Elysium-Labs-EU/eos/commit/561eef8d7b886907fbe3ba1cb4085617330c46e3))
- Remove duplicate assertion block in integration CRUD test ([`da8acbe`](https://github.com/Elysium-Labs-EU/eos/commit/da8acbefa222c3b7ff3211dceb070122ce3acca7))
- Fix stray error message and clarify FK-pragma test scope ([`b5c6976`](https://github.com/Elysium-Labs-EU/eos/commit/b5c6976c81bcc6920156936821c460bb59058217))
- Document PGID derivation in benchmark ([`07340e6`](https://github.com/Elysium-Labs-EU/eos/commit/07340e64f9b7b0bef66101dcdc5ea5664bbe67e7))
- Document synthetic /proc status fixture in benchmark ([`d8249e8`](https://github.com/Elysium-Labs-EU/eos/commit/d8249e8c4d84a93312e5a396767545a2dad96414))
- Clarify SUDO_USER test only proves code path, not resolution ([`2c13f08`](https://github.com/Elysium-Labs-EU/eos/commit/2c13f08e39064909958388c38a62fba470752f80))
- Document per-line timestamp behavior in TimestampWriter test ([`8ba844a`](https://github.com/Elysium-Labs-EU/eos/commit/8ba844a0d5706700f2c6574371e8d45e4c3a6ea4))
- Add real tests, replacing dead commented-out stubs ([`dc20eae`](https://github.com/Elysium-Labs-EU/eos/commit/dc20eae5a0a9f6401bf476e81207277a59a12a7e))
- Remove dead code and document shared API test fixtures ([`bead472`](https://github.com/Elysium-Labs-EU/eos/commit/bead472d1680cf70f7d4fc4ad12be81c58d37802))
- Trim redundant comments, clarify directory-add test intent ([`db99209`](https://github.com/Elysium-Labs-EU/eos/commit/db9920904de02ac8994612d4ef1b3bffd6ece476))
- Document startServiceForLogsTest as a shared logs-test fixture ([`7d361d3`](https://github.com/Elysium-Labs-EU/eos/commit/7d361d311ec444f38bf1a35995f86418e025a442))
- Note cross-file helper reuse in api remove test ([`80541c9`](https://github.com/Elysium-Labs-EU/eos/commit/80541c95e8bf1fc357b6a242da12d25e3633a07b))
- Clarify comments in api status tests ([`e56fdb0`](https://github.com/Elysium-Labs-EU/eos/commit/e56fdb0bc47a3eca36f0b5bdf6aeaede3c45d879))
- Trim redundant comments, document shared stop-test fixture ([`f43c9e7`](https://github.com/Elysium-Labs-EU/eos/commit/f43c9e7c63776fe6acb3d1d8f878c9f70c467776))
- Correct false claim in api update test comment ([`ba38596`](https://github.com/Elysium-Labs-EU/eos/commit/ba38596bbbbb4da709335dfcd7844f961e56a75a))
- Finish api validate comment fixes from task 34 ([`aca2185`](https://github.com/Elysium-Labs-EU/eos/commit/aca2185de4a841d9674ba8b6119e9b544b8e3f45))
- Correct TODO labels claiming a mock manager is required ([`687cdec`](https://github.com/Elysium-Labs-EU/eos/commit/687cdec4f0c3dcd28a2962c02e53a4dd803832ec))
- Rename misleading test, add coverage for run -f edge cases ([`19eaed0`](https://github.com/Elysium-Labs-EU/eos/commit/19eaed078d0c90309a7bc4193425c605bfa887ce))
- Clean up stop_test.go and add coverage for --force, no-mock gaps ([`745c60b`](https://github.com/Elysium-Labs-EU/eos/commit/745c60b2052a138b14ef681f300459d905eccda7))
- Remove dead test, add coverage for info.go's untested states ([`7523948`](https://github.com/Elysium-Labs-EU/eos/commit/7523948cf6df34daeb9db0e7e224195b643d77e9))
- Add coverage for print.go's untested render funcs ([`21b27f9`](https://github.com/Elysium-Labs-EU/eos/commit/21b27f9d8639b5db683efbc63f1c15d5b361b618))
- Add coverage for helpers.go's untested funcs ([`a82cdbd`](https://github.com/Elysium-Labs-EU/eos/commit/a82cdbdc353fe4b5f45a4781d57fe30476c5e2ed))
- Add coverage for completions.go's ServiceNameCompletions ([`d9685f3`](https://github.com/Elysium-Labs-EU/eos/commit/d9685f31d88b86a9c6745c68b93e69fd8f867e27))
- Add coverage for status.go's renderWatchFrame ([`fde9d15`](https://github.com/Elysium-Labs-EU/eos/commit/fde9d1582084b2163d9d8f968bc91eab511af8a2))
- Add coverage for daemon.go's standalone controller ([`44505f8`](https://github.com/Elysium-Labs-EU/eos/commit/44505f8a618c1fa4ccd21d628fcc98ced0afb808))
- Add coverage for api_daemon_logs.go ([`040fdb3`](https://github.com/Elysium-Labs-EU/eos/commit/040fdb3caf3dcc62780668f2722483d7aac27004))
- Add coverage for api_info.go's compileProcessInfoObject ([`31ea478`](https://github.com/Elysium-Labs-EU/eos/commit/31ea478d221118d5858eeda8e7abf6c394d1d098))
- Add coverage for system.go's uninstall path ([`30b02dd`](https://github.com/Elysium-Labs-EU/eos/commit/30b02dd5afb02ec762a55690cbdc3afaaf299fc9))
- Add coverage for remove.go and update.go gap cases ([`a51003b`](https://github.com/Elysium-Labs-EU/eos/commit/a51003b2043bda310285fad778f610603029e0cd))
- Add coverage for config.go's ValidateServiceConfig ([`4c14ffc`](https://github.com/Elysium-Labs-EU/eos/commit/4c14ffcef87cbc675f1ab3f99e9e24b4b4d1e51d))
- Add coverage for executor.go and daemon_manager.go gaps ([`694165a`](https://github.com/Elysium-Labs-EU/eos/commit/694165ac70680dfda087fc0d45c2ae5f1ef6587a))
- Add coverage for local_manager.go's WaitPipes, RestartService, UpdateServiceCatalogEntry ([`3215335`](https://github.com/Elysium-Labs-EU/eos/commit/3215335f01b8f9de0f89ccd4524e9f639660f5b1))
- Add coverage for service_log.go's health-log write path ([`2644366`](https://github.com/Elysium-Labs-EU/eos/commit/26443669e716ba2617cfd01a596e85f9e5780cf0))
- Add coverage for health_monitor.go's restartOnMemoryThreshold ([`92ab15d`](https://github.com/Elysium-Labs-EU/eos/commit/92ab15d97ad52e3693c1ebb877cef3945a99638f))
- Add coverage for evaluateMemoryThresholds and dispatchMemoryAction ([`d20fd4f`](https://github.com/Elysium-Labs-EU/eos/commit/d20fd4fb88ece51e499f56e24be58d6a134924b2))
- Add direct coverage for standalone daemon lifecycle funcs ([`473ddc8`](https://github.com/Elysium-Labs-EU/eos/commit/473ddc8a8eceab1315fe3a0f9615a5a2bdd6f0d6))

## [0.0.11-rc.23] - 2026-07-10

### Bug Fixes
- Reject stale XDG_RUNTIME_DIR owned by another user ([`abaa999`](https://github.com/Elysium-Labs-EU/eos/commit/abaa99971aec347ef08081c553518ec0c0591725))
- Resolve XDG_RUNTIME_DIR ownership against target user, not process uid ([`ad366db`](https://github.com/Elysium-Labs-EU/eos/commit/ad366db4f60c7d9c15598b3cd7ca4c4d4b2891b1))
- Warn when user bus is down before printing systemctl/journalctl hints ([`6da2343`](https://github.com/Elysium-Labs-EU/eos/commit/6da234384a48f488ad258d605a5dace43970b7cd))

## [0.0.11-rc.22] - 2026-07-10

### Bug Fixes
- Auto-heal user bus before systemctl stop ([`c2a2911`](https://github.com/Elysium-Labs-EU/eos/commit/c2a29111e0cb5fd2a0040916a007c41cb7a7e521))

## [0.0.11-rc.20] - 2026-07-10

### Features
- Auto-refresh installed shell completions on upgrade ([`f0f7bc4`](https://github.com/Elysium-Labs-EU/eos/commit/f0f7bc446bf633c8257077a667508330ea25ded6))


### Refactoring
- Split checkRunningProcess into focused helpers ([`f777628`](https://github.com/Elysium-Labs-EU/eos/commit/f77762835b344c1119991a5ac1ac8936fce0326b))

## [0.0.11-rc.19] - 2026-07-10

### Bug Fixes
- Resolve systemd scope from installed unit, not caller uid ([`561ce22`](https://github.com/Elysium-Labs-EU/eos/commit/561ce22534a88e9e7487f7e4ee481439cb8e44cd))


### Miscellaneous
- Bumps deps to latest version ([`012370e`](https://github.com/Elysium-Labs-EU/eos/commit/012370e54009c2343399c4a2d95e010cd3c94b90))

## [0.0.11-rc.18] - 2026-07-10

### Bug Fixes
- Auto-heal user bus failure and wire --verbose into startup/unstartup ([`84f3524`](https://github.com/Elysium-Labs-EU/eos/commit/84f35241ae93ec9cadaafdec353acd7d7c302d9b))


### Miscellaneous
- Service Orchestration Tool -> Service Supervisor ([`81726a8`](https://github.com/Elysium-Labs-EU/eos/commit/81726a8f533ed66f2af0f0045e602902ee8ec8a5))

## [0.0.11-rc.15] - 2026-07-08

### Bug Fixes
- Don't treat an already-dead daemon as a stop failure ([`9e90174`](https://github.com/Elysium-Labs-EU/eos/commit/9e90174170e6c1b4ddb591a181e93d9df882f5c2))

## [0.0.11-rc.14] - 2026-07-08

### Bug Fixes
- Fix renderServiceLogLine call signature in tests ([`b406dfe`](https://github.com/Elysium-Labs-EU/eos/commit/b406dfe7038bb4a1cbe9e132667b4f902178dceb))
- Route JSON output to stdout via OutOrStdout() ([`743da99`](https://github.com/Elysium-Labs-EU/eos/commit/743da99222a25a87588cf78cadf8d49f08b33b02))
- Make runtime type and path independently optional ([`9c41252`](https://github.com/Elysium-Labs-EU/eos/commit/9c412522ce6d74dc1f1021593981285963ad7efa))
- Fix 7 correctness bugs in sink process lifecycle ([`2c27c93`](https://github.com/Elysium-Labs-EU/eos/commit/2c27c93dd495b4fd12041de44666060751919368))


### Documentation
- Publish schema URL and add yaml-language-server hint ([`e58d024`](https://github.com/Elysium-Labs-EU/eos/commit/e58d024933785a5e2901fb2f36ac42339c921f81))
- Use consistent "service supervisor" terminology ([`34e825d`](https://github.com/Elysium-Labs-EU/eos/commit/34e825de6a5ba3afb53bc7d99502fd295d43c0a7))
- Add GitHub Actions deploy section ([`8ead815`](https://github.com/Elysium-Labs-EU/eos/commit/8ead8150530208c2f0a9ec31833075b2210e6f6e))
- Add CONTRIBUTING guide and PR template ([`c55a8c3`](https://github.com/Elysium-Labs-EU/eos/commit/c55a8c3d8e26457623da2b843f982248745324c5))
- Add log sinks section; replace em-dash with semicolon ([`0684056`](https://github.com/Elysium-Labs-EU/eos/commit/0684056c8c2931caf59856f05444c1ef04493f46))


### Features
- Introduce Monitor and monitorManager interfaces ([`8d89c4a`](https://github.com/Elysium-Labs-EU/eos/commit/8d89c4a1392a0c966a9d9fe81b054d9e97ac7947))
- Configurable check interval test coverage and docs ([`c8a6412`](https://github.com/Elysium-Labs-EU/eos/commit/c8a64122813673d0da6574d37c50dc5d9b2595a1))
- Add eos init command ([`9968f1e`](https://github.com/Elysium-Labs-EU/eos/commit/9968f1e666dfe7944fad88e139589c62dc0dfd24))
- Suggest shell completion setup after install ([`be97083`](https://github.com/Elysium-Labs-EU/eos/commit/be970833fee34eb8ac8fd51bc55842f743801423))
- Add full subcommand coverage with tests ([`45dfa00`](https://github.com/Elysium-Labs-EU/eos/commit/45dfa00b3f16df3c2a93302cfb49967f5f2c453c))
- Add interactive shell completion installer ([`a639f1e`](https://github.com/Elysium-Labs-EU/eos/commit/a639f1ee3f929a505b584e81a879afbec55cf561))
- Add log sink plugin system ([`0c13586`](https://github.com/Elysium-Labs-EU/eos/commit/0c135865385e9146cb3df62e15d3522711848d8d))
- Wire mode, address, and error log routing through sink pipeline ([`5e943b9`](https://github.com/Elysium-Labs-EU/eos/commit/5e943b91e61e6b72ed8d61590a4b037bb1e288f6))


### Maintenance
- Remove bundled logbench plugin; moved to eos-plugins repo ([`d70c440`](https://github.com/Elysium-Labs-EU/eos/commit/d70c440a7c36baeeafb3391ecce60f599ea34e5a))
- Merge main; keep Log Sinks and Deploy with GitHub Actions sections ([`9fdac33`](https://github.com/Elysium-Labs-EU/eos/commit/9fdac3386c1185e43bbfef17dd88e79973d0984a))


### Miscellaneous
- Replace em-dashes with semicolons in comments ([`cccdb85`](https://github.com/Elysium-Labs-EU/eos/commit/cccdb85686158a4c319bc9878ebc86004bc8dfdd))
- Fix space before semicolons in comments ([`5f0dcc2`](https://github.com/Elysium-Labs-EU/eos/commit/5f0dcc2c936d97177acc17edbaab9be5d389e7e1))


### Refactoring
- Replace skipManagerInit allowlist with lazy sync.Once init ([`74ad65c`](https://github.com/Elysium-Labs-EU/eos/commit/74ad65c7c113cfb3cd56560a83e9c14a2bde0f69))


### Testing
- Add unit tests for daemon start --detach/-d flag ([`7876de1`](https://github.com/Elysium-Labs-EU/eos/commit/7876de1243b1bdcffb1bcbae8688ad96291ce494))
- Add three-tier tests for sink plugin system ([`b9ac695`](https://github.com/Elysium-Labs-EU/eos/commit/b9ac6955d404714b5b100bda90e0424c297a9f79))
- Cover fixed lifecycle paths in sink process and ring buffer ([`22cba93`](https://github.com/Elysium-Labs-EU/eos/commit/22cba93dc915bfec1c6b17e8322395ea5855ef92))

## [0.0.11-rc.13] - 2026-07-04

### Features
- Add demo GIF to README ([`3aaa36d`](https://github.com/Elysium-Labs-EU/eos/commit/3aaa36df4e8518c0001125e542d4000d285b7d90))
- Harmonize log commands across service and daemon ([`f3be059`](https://github.com/Elysium-Labs-EU/eos/commit/f3be059e74133d929b8baeb71be72e69771f37e9))
- Add features section, env config, and service yaml examples ([`af31710`](https://github.com/Elysium-Labs-EU/eos/commit/af317108a688a33a4520294275c5c7cd8ce710ec))


### Miscellaneous
- Tweaks to readme ([`218a86e`](https://github.com/Elysium-Labs-EU/eos/commit/218a86ead20738e3273d7c5e40eeed1fd87215a7))


### Testing
- Add P1 unit tests and enforce 40% coverage threshold ([`66fc4ec`](https://github.com/Elysium-Labs-EU/eos/commit/66fc4ecd47f4e24ea2f8414b473cf923d3818bd8))
- Add P2 unit tests for scanStatusFieldBytes and checkUnknownProcess ([`81a594b`](https://github.com/Elysium-Labs-EU/eos/commit/81a594b2838496ffa79b3b34356343e4fba2aa67))
- Add P3 unit tests for lifecycle, stop, and runtime path ([`a0c83a2`](https://github.com/Elysium-Labs-EU/eos/commit/a0c83a284b0e390e97cbfe632a49a6640370d628))
- Add P4 IPC socket tests for DaemonManager ([`c4c4909`](https://github.com/Elysium-Labs-EU/eos/commit/c4c490965407dbe0fb1c84b3e5cca4e35564d1a6))
- Add P5 HTTP mock tests for system update flow ([`dc90152`](https://github.com/Elysium-Labs-EU/eos/commit/dc901522fe20073c4663344570f30bcdf30decbf))
- Fix correctness issues and remove dead code ([`338056f`](https://github.com/Elysium-Labs-EU/eos/commit/338056f2d9c794e8560627379b3bda348c3796f4))

## [0.0.11-rc.12] - 2026-07-03

### Bug Fixes
- Verify SHA256 checksum before installing binary ([`7baa4bf`](https://github.com/Elysium-Labs-EU/eos/commit/7baa4bf11ecf063021bdf12daa4e3c85c6feba6b))
- Preserve last RSS value during throttled mem sample ticks ([`fed938e`](https://github.com/Elysium-Labs-EU/eos/commit/fed938e86b3a357899ed4578aa6811e00b850169))


### Maintenance
- Replace Docker test targets with OrbStack equivalents ([`316f4b6`](https://github.com/Elysium-Labs-EU/eos/commit/316f4b681bd3f7e064f493070934874e84f25445))

## [0.0.11-rc.11] - 2026-07-03

### Bug Fixes
- Fetch checksum from sha256sums.txt instead of API digest field ([`b2dc515`](https://github.com/Elysium-Labs-EU/eos/commit/b2dc5150e3a23c11a13c00c60ed331629c185e72))

## [0.0.11-rc.10] - 2026-07-03

### Bug Fixes
- Connect directly to DB in systemd mode instead of erroring ([`e18f1c2`](https://github.com/Elysium-Labs-EU/eos/commit/e18f1c25c6bcac60ea7e7548be1d19852e4ff9c2))


### Features
- Migrate to slog with --verbose flag and e2e tests ([`af48a94`](https://github.com/Elysium-Labs-EU/eos/commit/af48a94c786d2863292b5b7cd40a695dd0681c43))
- Store service logs as JSON slog, render on eos logs ([`08205d0`](https://github.com/Elysium-Labs-EU/eos/commit/08205d0a1aa838fa231d672936ec861341fdee13))

## [0.0.11-rc.9] - 2026-07-03

### Bug Fixes
- Reconcile orphan processes on daemon startup ([`663e837`](https://github.com/Elysium-Labs-EU/eos/commit/663e83740823ca1b34222d91f747500176386fbb))


### Features
- Prompt to stop running daemon before install ([`50aa56d`](https://github.com/Elysium-Labs-EU/eos/commit/50aa56d3e12cd5e4e8841f166e67782be555b3e2))
- Auto-detect system vs user systemd unit for startup/unstartup ([`b5043dc`](https://github.com/Elysium-Labs-EU/eos/commit/b5043dc047d0c7d0112ab4c22d7d61d412711644))
- Add DB seed helpers, bench suite, and golangci-lint worktree fix ([`4bc4236`](https://github.com/Elysium-Labs-EU/eos/commit/4bc4236f9c3ebae43f216633fab481cec4b99578))

## [0.0.11-rc.8] - 2026-07-02

### Features
- Add Python runtime support ([`96749b5`](https://github.com/Elysium-Labs-EU/eos/commit/96749b5d0f845ad78c6ae9bdfc911a5381f4dcba))
- Add ~/.eos/config.yaml for daemon tunables ([`922bf0b`](https://github.com/Elysium-Labs-EU/eos/commit/922bf0bccff2f8ffdf153fc0e79ea6b84b7ec06a))
- Add env var overrides for all eos config tunables ([`bcf50fb`](https://github.com/Elysium-Labs-EU/eos/commit/bcf50fb8304bf545b7b966e8fe4cf0d1f67a128b))
- Add --watch / -w flag to status command ([`dade699`](https://github.com/Elysium-Labs-EU/eos/commit/dade6994c6e082a0209ecc2df3e5cd023ec7c2d8))
- Prompt user before removing a non-stopped service ([`c2d1f85`](https://github.com/Elysium-Labs-EU/eos/commit/c2d1f85d2118099baad86dd71a97d24f34689e90))


### Performance
- Reduce monitor allocations and add mem sample throttle ([`8ced47e`](https://github.com/Elysium-Labs-EU/eos/commit/8ced47e236a8a47e95634fe96a7ac92e9347f951))
- Eliminate 3 allocs per tick in isProcessAlive ([`600c7c8`](https://github.com/Elysium-Labs-EU/eos/commit/600c7c8a703fd5ea31f33f8e39925bb052f68855))
- Eliminate allocs in isProcessAlive and checkMemoryLinux ([`e0713ce`](https://github.com/Elysium-Labs-EU/eos/commit/e0713ce5692566fac748b0edb408bbbea5cc9ff2))
- Eliminate per-tick allocations in DB hot paths ([`0967408`](https://github.com/Elysium-Labs-EU/eos/commit/09674089d74f2729397628be9a7d60e7b5b0c5a4))

## [0.0.11-rc.7] - 2026-06-30

### Bug Fixes
- Memory limit restarts now respect maxRestartCount and backoff ([`3abf49c`](https://github.com/Elysium-Labs-EU/eos/commit/3abf49c2aec61217487eae6717f82f572e8fac5c))
- Eliminate Unknown transient state and guard nil StartedAt ([`365bd67`](https://github.com/Elysium-Labs-EU/eos/commit/365bd677317dd9f2ab1efc16e333eb890fef284d))
- Honour ctx cancellation in HealthMonitor, drop Stop() ([`030b20b`](https://github.com/Elysium-Labs-EU/eos/commit/030b20b67a5b1cd87bd327d67fa5a9aa4527ab91))
- Prevent root-owned ~/.eos when eos run as root ([`3e8229c`](https://github.com/Elysium-Labs-EU/eos/commit/3e8229ccb01cc0a57e9637aea9d50473e37a3dd9))
- Guard nil StartedAt in DetermineUptimeHuman and DetermineUptimeAPI ([`7dd85f4`](https://github.com/Elysium-Labs-EU/eos/commit/7dd85f47fc7bb1c0b23ba1358ab8c4cbd55fa303))
- Use hardcoded test config in newTestRootCmd instead of reading from disk ([`a123cc4`](https://github.com/Elysium-Labs-EU/eos/commit/a123cc4512d80a5aa06d51afa0e67242d2e5dd8b))


### CI/CD
- Drop nilaway — OOM-killed on free Codeberg runners ([`1979509`](https://github.com/Elysium-Labs-EU/eos/commit/19795091865b5246ecd7b03ed822e19712d06eb1))
- Remove empty nilcheck job ([`152da92`](https://github.com/Elysium-Labs-EU/eos/commit/152da92dc272067aead43f312704ffa5e5ba2ba3))


### Documentation
- Restructure README for clarity and user focus ([`d1b4eac`](https://github.com/Elysium-Labs-EU/eos/commit/d1b4eaced06d1e4a2b0c95860a0f6ad1ad2e1b55))


### Features
- Reset restart counter after stable uptime window ([`0b4dae5`](https://github.com/Elysium-Labs-EU/eos/commit/0b4dae59397a90171350f5618f5d5ca048adc044))
- Abstract exec calls behind Executor interface ([`5b6ac58`](https://github.com/Elysium-Labs-EU/eos/commit/5b6ac58f0987f2c27db7879b545e2faf475b6142))
- Add eos validate command ([`6b9ce4c`](https://github.com/Elysium-Labs-EU/eos/commit/6b9ce4c2b8a7a4b4752efdc10c1a22ae48d0d6e1))
- Collect all validation errors in validate command ([`4b18854`](https://github.com/Elysium-Labs-EU/eos/commit/4b18854b376b94979c730f6d064fa61f48c48e0f))


### Testing
- Fix TestNewSystemConfigHelper when run as root ([`83acf66`](https://github.com/Elysium-Labs-EU/eos/commit/83acf668a194519bf1f07f1759fd333a6ebc5d62))
- Set EOS_BASE_DIR in setupCmd to fix root-env test failures ([`1d6527d`](https://github.com/Elysium-Labs-EU/eos/commit/1d6527d543bd5563836e15e4635ce0367bbe4ef6))

## [0.0.11-rc.6] - 2026-06-29

### Bug Fixes
- Address security and reliability findings from CodeRabbit review ([`6b1c095`](https://github.com/Elysium-Labs-EU/eos/commit/6b1c0951bc2923273e5ee890d335ae6812620210))
- Address minor CodeRabbit findings across cmd and internal packages ([`97ec073`](https://github.com/Elysium-Labs-EU/eos/commit/97ec0736f49971403b87fb8821e3a1f442805d6e))
- Use upload-artifact@v3 in bench CI job ([`c30c64a`](https://github.com/Elysium-Labs-EU/eos/commit/c30c64a38cf9b719b68f55dcc70738258ac2a38d))


### CI/CD
- Use go-version-file and migrate issue templates to forgejo ([`1fa6578`](https://github.com/Elysium-Labs-EU/eos/commit/1fa6578bbdb037b567ec0f4e5965af90eb1922f7))


### Features
- Add memory/CPU profiling infra and fix daemon cmd testability ([`db2356e`](https://github.com/Elysium-Labs-EU/eos/commit/db2356e941dd26eecf89103556b9813207bf6cd5))


### Maintenance
- Expand linter rules, add ast-grep, and improve dev tooling ([`8991a4e`](https://github.com/Elysium-Labs-EU/eos/commit/8991a4e9c833fcb83cc514619dcd257769e7a1fa))


### Refactoring
- Restructure daemon config and thread ctx through Stop ([`4756fd9`](https://github.com/Elysium-Labs-EU/eos/commit/4756fd9f777f72570a143dd27be90cd885312003))


### Testing
- Add goroutine leak detection and fix test teardown ([`b849e25`](https://github.com/Elysium-Labs-EU/eos/commit/b849e257736ef7af43796e86a58b0783c33ffd05))
- Inject executor into startup/unstartup cmds and add tests ([`4ac2fe7`](https://github.com/Elysium-Labs-EU/eos/commit/4ac2fe73b6d0a5dccdbbb5be3e3656c20522ca69))
- Fix goroutine leaks in pipe-forwarding goroutines ([`65e74e5`](https://github.com/Elysium-Labs-EU/eos/commit/65e74e5d4d1084ff29a3a823e10e48eaf8704301))

## [0.0.11-rc.5] - 2026-05-03

### Features
- Adds env file parsing to building the environment ([`e37c1b6`](https://github.com/Elysium-Labs-EU/eos/commit/e37c1b6ffbc13fa86a10ca7ef8a04744b78d957f))
- Adds boot persistence via systemd ([`8386c40`](https://github.com/Elysium-Labs-EU/eos/commit/8386c408d1fcef2e30cb6eccee7f9ded4d7f947f))


### Maintenance
- Adds some test for environment handeling ([`bdad375`](https://github.com/Elysium-Labs-EU/eos/commit/bdad3759ed25589ba4f649cdeef18a9cee060413))

## [0.0.11-rc.4] - 2026-04-18

### Features
- Adds DSN to database driver for timeout and writer logs ([`b5cecc7`](https://github.com/Elysium-Labs-EU/eos/commit/b5cecc7c3ffb9f54ec75752ef00d9faf96454606))


### Improvements
- Updates dependencies to latest versions ([`6202309`](https://github.com/Elysium-Labs-EU/eos/commit/62023092ef06b8abba54e4ba35e3d6a7e8f72228))


### Maintenance
- Fixes broken install.sh and adjusts readme ([`6c8cdda`](https://github.com/Elysium-Labs-EU/eos/commit/6c8cddaf0f8c5abaa6d72fcb8ef5cf5930679b53))


### Miscellaneous
- Enhances CLI text output coloring ([`79c1340`](https://github.com/Elysium-Labs-EU/eos/commit/79c1340da78632969fc84e0c1d9561eb0b57a81c))

## [0.0.11-rc.3] - 2026-04-10

### Bug Fixes
- Fixes invalid repo in install script ([`b5364a6`](https://github.com/Elysium-Labs-EU/eos/commit/b5364a6b5c823f4ffadcb81283a22e002058ca77))


### Maintenance
- Remaps all github urls to codeberg ([`e5c40bd`](https://github.com/Elysium-Labs-EU/eos/commit/e5c40bd91f6f3780ac3bcb16b5e9e06999edbcb7))

## [0.0.11-rc.2] - 2026-04-06

### Bug Fixes
- Changes the runner in action to codeberg variants ([`ac515dc`](https://github.com/Elysium-Labs-EU/eos/commit/ac515dcb14a52a89970a50c681f0490512ac6f22))
- Various bug fixes ([`7afc6b7`](https://github.com/Elysium-Labs-EU/eos/commit/7afc6b7071c77ef118ca35103e52ed925af12c89))
- Various bug fixes ([`8925706`](https://github.com/Elysium-Labs-EU/eos/commit/8925706a2e2362adb25045eda672451f13131c72))
- Various bug fixes ([`376fee8`](https://github.com/Elysium-Labs-EU/eos/commit/376fee88f36934a5109474c12a6891212a0736b4))
- Invalid codeberg references ([`af6bd66`](https://github.com/Elysium-Labs-EU/eos/commit/af6bd6657508eeb4d4aee03b43923b8de90d465b))
- Update action in release pipeline to match forgejo capabilities ([`82a2590`](https://github.com/Elysium-Labs-EU/eos/commit/82a2590a3071ae84f871cac48a71ef711593af90))


### Features
- Api version of info command ([`da142eb`](https://github.com/Elysium-Labs-EU/eos/commit/da142eb447ab880b56a2b6ba3ad26e33111a202b))


### Improvements
- Update ISSUES.md ([`69296e1`](https://github.com/Elysium-Labs-EU/eos/commit/69296e1bed3363396431622d5350500f602bf947))


### Maintenance
- Moves project references from GitHub to Codeberg ([`71e7bb1`](https://github.com/Elysium-Labs-EU/eos/commit/71e7bb1a488e2fa0109de1c6e767c42c89993e37))
- Add manual push option to codeberg workflows ([`9995189`](https://github.com/Elysium-Labs-EU/eos/commit/9995189183da70228a8c8a415921607c6616f6ce))
- Adjusts version of golangci tool ([`549025d`](https://github.com/Elysium-Labs-EU/eos/commit/549025d72954e8479ede2ed40956686b44b7ac1c))
- Centralizes error messages, and handles sentinel errors in daemon communication ([`ee2da1f`](https://github.com/Elysium-Labs-EU/eos/commit/ee2da1f0ac0517643d7d25d4f2faf5d4c3ce573f))

## [0.0.10] - 2026-04-05

### Bug Fixes
- Fix ldflags package path for version injection ([`48766dd`](https://github.com/Elysium-Labs-EU/eos/commit/48766dd221b1dcd7d6de51dd57246c075668e547))
- Fixes invalid test case expecting input ([`e3d23e9`](https://github.com/Elysium-Labs-EU/eos/commit/e3d23e9199f38b85ec9c4ebf15f9e6c4f2a3a08d))
- Fixes invalid tests cases for killing processes ([`c352457`](https://github.com/Elysium-Labs-EU/eos/commit/c352457bfc05ce200d3840c5dc78cdcfaed0be57))


### Improvements
- Improves daemon socket handeling ([`0d17e8d`](https://github.com/Elysium-Labs-EU/eos/commit/0d17e8d947271b7f6b5f5ecf120d3cc43b6d8b24))

## [0.0.11-rc.1] - 2026-04-04

### Features
- Adds uninstall command to system ([`cc1c0cb`](https://github.com/Elysium-Labs-EU/eos/commit/cc1c0cbe144da9983a37ecfb9356e2fe9947d63e))


### Improvements
- Improves CLI feedback ([`633677b`](https://github.com/Elysium-Labs-EU/eos/commit/633677bc0257b6f1d582b27b3ec71966eaa70913))

## [0.0.10-rc.9] - 2026-04-03

### Miscellaneous
- Changes module name to enable pkg.go.dev indexing ([`2eda9d6`](https://github.com/Elysium-Labs-EU/eos/commit/2eda9d6ecffb3e013878a3176be82f705efccfaf))

## [0.0.10-rc.8] - 2026-04-03

### Features
- Adds api versions of run and logs commands ([`22ed947`](https://github.com/Elysium-Labs-EU/eos/commit/22ed947ae3402e11b25d5257abe74a525aa39846))

## [0.0.10-rc.7] - 2026-04-03

### Improvements
- Updates the overall status table handeling - to allow stopped services ([`9e363d1`](https://github.com/Elysium-Labs-EU/eos/commit/9e363d196bfbaff5567b253be492c6de818bac4c))
- Updates readme with new run command ([`cdd4e86`](https://github.com/Elysium-Labs-EU/eos/commit/cdd4e8667eb776c3cb01fe53c5b1e7b6312633c2))
- Updates go version build pipelines ([`0d810c9`](https://github.com/Elysium-Labs-EU/eos/commit/0d810c9af7b56b80e89f376015b3347a74333e9a))


### Miscellaneous
- Memory check and limit setting available, improved CLI with examples, autocomplete and long desc ([`21a4b68`](https://github.com/Elysium-Labs-EU/eos/commit/21a4b6865e05d2ae74fd99b52f4bb5bd5134e2de))

## [0.0.10-rc.6] - 2026-03-15

### Miscellaneous
- Restores service log functionality with new pgid approach ([`325f599`](https://github.com/Elysium-Labs-EU/eos/commit/325f5999c4bb339b676ffadda362f051284d147c))

## [0.0.10-rc.5] - 2026-03-15

### Miscellaneous
- Creates new run command - which will replace the start and restart commands ([`f86f8b4`](https://github.com/Elysium-Labs-EU/eos/commit/f86f8b4d0973d05781881a749792800a53f0d9d9))

## [0.0.10-rc.4] - 2026-03-10

### Bug Fixes
- Fixes invalid file descriptor issue for system update ([`d34b061`](https://github.com/Elysium-Labs-EU/eos/commit/d34b0610aa2763a73889174071766c30fd7594bd))

## [0.0.10-rc.3] - 2026-03-09

### Miscellaneous
- Changes tracking processes from PID to PGID ([`aad9552`](https://github.com/Elysium-Labs-EU/eos/commit/aad955298f4e549f7dd59a4ea77d2efd4978bf0d))

## [0.0.10-rc.2] - 2026-03-07

### Improvements
- Improves update with precheck on backup folder access ([`906b7b6`](https://github.com/Elysium-Labs-EU/eos/commit/906b7b6a04da73d68677411dad7f850368a05aaa))
- Updates deps + updates linting in pipeline ([`efba6d9`](https://github.com/Elysium-Labs-EU/eos/commit/efba6d9f088385d69b7a9803d6c74348d99b006c))
- Updates linting in pipeline ([`88089e9`](https://github.com/Elysium-Labs-EU/eos/commit/88089e92d230dc84f8532d45df70e9efb557c176))

## [0.0.10-rc.1] - 2026-03-03

### Bug Fixes
- Fixes health_monitor tests failing in linux ([`3042389`](https://github.com/Elysium-Labs-EU/eos/commit/30423897eb7fdd311d1e47817e9fc1ac9f228da1))


### Miscellaneous
- Adjusts daemon protocol + rewrites process stopping ([`1b1843f`](https://github.com/Elysium-Labs-EU/eos/commit/1b1843f6f3492763d563d76e02a36fe1d43e5101))
- Adjusts test suite to address failing tests ([`362e1e0`](https://github.com/Elysium-Labs-EU/eos/commit/362e1e09709a8e0db427043d534a47313225c564))
- Removes obsolete claude branches ([`5a05161`](https://github.com/Elysium-Labs-EU/eos/commit/5a0516149af267882bd6edbc2d482019980c9535))
- Simplifies tests to enable cross OS test results ([`3bbfe8c`](https://github.com/Elysium-Labs-EU/eos/commit/3bbfe8c1d64a01c9fb66a1be066a63cbd691fd67))

## [0.0.9-rc.2] - 2026-02-25

### Bug Fixes
- Fixes invalid test for system update ([`96e8603`](https://github.com/Elysium-Labs-EU/eos/commit/96e860356a0f73d0d4850a7ffd46b0f4b6f6de09))

## [0.0.9-rc.1] - 2026-02-25

### Features
- Adds support for pre-releases in building and consuming ([`be27d61`](https://github.com/Elysium-Labs-EU/eos/commit/be27d61780722474c7d2f5c3bc6419255463b7b8))

## [0.0.8] - 2026-02-25

### Features
- Adds request for daemon restart after binary update + adds more info to daemon info command ([`e71b3bc`](https://github.com/Elysium-Labs-EU/eos/commit/e71b3bc182faf5b3a67ed0296daab6925a2dc750))

## [0.0.7] - 2026-02-25

### Improvements
- Improves log output for services ([`4db7ba1`](https://github.com/Elysium-Labs-EU/eos/commit/4db7ba185d50f9139d1f97fdb4ab7d2f7fa19199))

## [0.0.6] - 2026-02-25

### Improvements
- Improves log output for services and daemon ([`91c302e`](https://github.com/Elysium-Labs-EU/eos/commit/91c302e4d0d9235571ec1d0f951af87396e486cc))

## [0.0.5] - 2026-02-25

### Improvements
- Improves system update process to adhere to linux file rules ([`b8a52ea`](https://github.com/Elysium-Labs-EU/eos/commit/b8a52eae481d8bb100b7c6c530484d7c8fdbc20b))

## [0.0.4] - 2026-02-25

### Miscellaneous
- Enhances cli experience with improved messages ([`4e6e4c7`](https://github.com/Elysium-Labs-EU/eos/commit/4e6e4c7680d417b176e46c06000b656684c0cfbb))

## [0.0.3] - 2026-02-24

### Improvements
- Updates release pipeline ([`aac8bb2`](https://github.com/Elysium-Labs-EU/eos/commit/aac8bb2bf640dda9f60425f20a09ceb57a7d4509))
- Updates buildinfo handeling + allows for local binary installation via install.sh ([`f3ee51c`](https://github.com/Elysium-Labs-EU/eos/commit/f3ee51c001d3ffc8554f0afe084edcf3478e5cbc))

## [0.0.2] - 2026-02-24

### Bug Fixes
- Fixes mismatch in requirements for ci pipeline ([`f159cdf`](https://github.com/Elysium-Labs-EU/eos/commit/f159cdf00b39ec7b52902081ae654f6ffbc94f9b))
- Fixes timezone mismatch in different environment for tests ([`7718788`](https://github.com/Elysium-Labs-EU/eos/commit/771878833cc9e76ef8bb063dafbf39efa80e21a3))


### Improvements
- Update README.md ([`f54c5c9`](https://github.com/Elysium-Labs-EU/eos/commit/f54c5c976c41130f6d00442eacf86c18e0428765))
- Updates install shell commands + adds Makefile with useful shorthands ([`20eb879`](https://github.com/Elysium-Labs-EU/eos/commit/20eb879bf4d0f487f9f21f08d8bf7785e9c06a8f))


### Miscellaneous
- Clarify config management in README

Removed mention of database sync in the README. ([`2eef37c`](https://github.com/Elysium-Labs-EU/eos/commit/2eef37ceeb9f553412a9c0ab262841ec25d94c46))
- Tweaks to Github Actions + improved test coverage + changes based on linting rules ([`6dbf0e4`](https://github.com/Elysium-Labs-EU/eos/commit/6dbf0e4ea8953b2ff6164c21c4af291c606042d9))
- Disables port checking and related tests + adds improved handeling for error cases ([`714affb`](https://github.com/Elysium-Labs-EU/eos/commit/714affb4d74e89f7b5dc185702134670f67db81c))
- Adjusts ci pipeline by splitting linting into own and removing the codecoverage upload ([`e7db799`](https://github.com/Elysium-Labs-EU/eos/commit/e7db79958aca1061bf8881dc6d99eff3d7954283))
- Pins golangci version in pipeline to match local + adds additional fixes to precommit ([`b47c6b5`](https://github.com/Elysium-Labs-EU/eos/commit/b47c6b58f6bdf81db418f566e4cd9f1872b28bbc))


### Refactoring
- Refactors codebase to new linting standards ([`a7e88f1`](https://github.com/Elysium-Labs-EU/eos/commit/a7e88f154c97d10f0220ec5e10cbe801973d6416))

## [0.0.1] - 2026-01-25

### Bug Fixes
- Fixes invalid sqlite package reference ([`cb3edb1`](https://github.com/Elysium-Labs-EU/eos/commit/cb3edb13e8b6850e70954d478798e1b56cb6d0af))


### Features
- Adds github deploy pipeline to the project ([`1168f3a`](https://github.com/Elysium-Labs-EU/eos/commit/1168f3aa6b3871874fbd0bab84d15aab6c6e140c))
- Adds Apache 2.0 license and updates readme ([`eb2053b`](https://github.com/Elysium-Labs-EU/eos/commit/eb2053b19ee25d75369758054bbbc7fa611d5f85))
- Adds database migrations and migration tests ([`838b15c`](https://github.com/Elysium-Labs-EU/eos/commit/838b15cb1fd3dec4671243552cf44e4c0f7c2787))


### Improvements
- Improves cli output with new lines ([`25e4716`](https://github.com/Elysium-Labs-EU/eos/commit/25e47164c40d736ac37cea7cb573d8542479f2b9))


### Miscellaneous
- Initial commit ([`0078430`](https://github.com/Elysium-Labs-EU/eos/commit/00784306bdb84d99bf0a9a96ef74c309f964be9f))
- Updating main package references ([`8bf0d19`](https://github.com/Elysium-Labs-EU/eos/commit/8bf0d197081c4f481a667c4834fb7ed8be92860a))

