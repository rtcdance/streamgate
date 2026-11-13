# Changelog

All notable changes to StreamGate are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and the project uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## 1.0.0 (2026-11-13)


### Features

* **acceptance:** add the durable-only internal fault barrier ([9a1d4e0](https://github.com/rtcdance/streamgate/commit/9a1d4e0b388adde299f8467bd0c9fb6a131fe9db))
* **acceptance:** drive the durable fault matrix against a real stack ([5910f55](https://github.com/rtcdance/streamgate/commit/5910f5589004af97408f383e9fd681654e3d1808))
* **acceptance:** wire the durable fault barrier into the worker, publisher and indexer ([9cdc384](https://github.com/rtcdance/streamgate/commit/9cdc38402ef55e6421445a7170965fff0d075b3b))
* **auth:** add the Postgres-authoritative restore epoch (batch C, part 1) ([ed8f3f1](https://github.com/rtcdance/streamgate/commit/ed8f3f165a2762ea0ac13460ccc7a9e5c2e0c15a))
* **auth:** bind every credential to a session and auth epoch (SG-06 step 2) ([6f993bf](https://github.com/rtcdance/streamgate/commit/6f993bfa41e780d846c6ad6208d319d9e1a141e1))
* **auth:** make session epochs durable and cached with a hard 5s bound ([0fcddee](https://github.com/rtcdance/streamgate/commit/0fcddeea8ab1c09ae814e04b6f82b81c3e754900))
* **authz:** one authorization flow for both HTTP and gRPC (batch B) ([fa5d96f](https://github.com/rtcdance/streamgate/commit/fa5d96f99e67ef7b4ae303c5040e9e2ae351a845))
* **capability:** carry the rule version so a rule change reaches media in flight (SG-06) ([aed2132](https://github.com/rtcdance/streamgate/commit/aed21324c69f5ce4f7a2dda717123bff124d0fdd))
* **capability:** derive and enforce the 70s transfer bound; refuse a mismatched JWT ([4de6cc9](https://github.com/rtcdance/streamgate/commit/4de6cc918b9c79105001f18ecf92cf88a6729703))
* **capability:** every playlist and segment checks the revocation version (SG-06 step 4) ([08ab81e](https://github.com/rtcdance/streamgate/commit/08ab81ede79178f9727cf29b25e97dc062bf0dc1))
* **capability:** give playback capabilities their own audience ([faeb3df](https://github.com/rtcdance/streamgate/commit/faeb3df0872b02fe3fc3089264d31b515098ffae))
* **ci:** emit the structured candidate manifest §7 requires ([8b47cb9](https://github.com/rtcdance/streamgate/commit/8b47cb983f21efcefcf350ad6e275dd8fedc5417))
* **ci:** run the rollback-compatibility leg in call mode too (SG-08 step 6) ([6962fca](https://github.com/rtcdance/streamgate/commit/6962fca9bcc8c3ab2de33b9d335bc995f8ec6f9d))
* **ci:** wire the D node with three-class failure drills (SG-08 step 5) ([4457eb0](https://github.com/rtcdance/streamgate/commit/4457eb092a13104bf23cee9c8cd740fce2a376f9))
* **events:** bounded NFT event pipeline with durable checkpoints (SG-13 B) ([3541cc9](https://github.com/rtcdance/streamgate/commit/3541cc9132ea6e3fd182a64c56d31b86cca10deb))
* **gating:** ruleVersion as the authority for "have the rules changed?" (SG-06 step 3) ([9234d08](https://github.com/rtcdance/streamgate/commit/9234d084be325a118b52c10c112ad876c117ed23))
* **gating:** start the event indexer on the first gating rule ([e9390ab](https://github.com/rtcdance/streamgate/commit/e9390abd83f873610124802d91c754e210cab761))
* **observability:** bound gate labels and record real probe/RTF measurements ([86d01cb](https://github.com/rtcdance/streamgate/commit/86d01cb9357a06536b9afe6f6ba3f46fce254971))
* **ops:** explicit profiles, key separation, readiness, and scoped limits (SG-14) ([66a6f25](https://github.com/rtcdance/streamgate/commit/66a6f2547df55a4ed53557b116b3494cec3372bd))
* **ops:** refuse to run production on silent in-memory fallbacks (SG-14 step 1) ([6b56f1a](https://github.com/rtcdance/streamgate/commit/6b56f1a40a266cf6a320eaae22d66caac9975741))
* **perf:** implement SG-07 step 2 as written ([5ba78ee](https://github.com/rtcdance/streamgate/commit/5ba78eece0a7362013a1650db5e8f2e124d83c8e))
* **perf:** inject the SG-07 fault matrix instead of labelling it ([d18c50d](https://github.com/rtcdance/streamgate/commit/d18c50d1f31a6e2af7a238a882b20d74815f9156))
* **perf:** isolate viewer load and capture bounded runtime evidence ([f6fd2c8](https://github.com/rtcdance/streamgate/commit/f6fd2c837f9d7aa32c4285da559890198180c3f8))
* **perf:** make SG-07 step 5's p95 regression limit real and runnable ([f787dae](https://github.com/rtcdance/streamgate/commit/f787daed981644c778c4dd10b95a149fef2188d2))
* **perf:** per-viewer wallet pool with paced logins (SG-07) ([91a81be](https://github.com/rtcdance/streamgate/commit/91a81be9a15854d73b2315c9b8d5041259e31c5f))
* **perf:** real HTTP load harness and honest throughput evidence (SG-07 code) ([dfe07e0](https://github.com/rtcdance/streamgate/commit/dfe07e0aebe4b97dcf81fdec04d29cff03549e2c))
* **perf:** sample real CPU, RSS and app metrics per scenario ([4a74847](https://github.com/rtcdance/streamgate/commit/4a7484758f80a9419f9e005834e5ce353fd9c9b5))
* **perf:** support the SG-07 matrix repeats and the capacity exploration ([63e0c05](https://github.com/rtcdance/streamgate/commit/63e0c05d86992ee285c50c891dbc8dabaa3f9af1))
* **registry:** durable on-chain registration intents (SG-13 A) ([e5b2a29](https://github.com/rtcdance/streamgate/commit/e5b2a29c8b72d1ff147f438cec1bd143755a7524))
* **registry:** verify the deployment identity and sign with the configured key (batch C, part 2) ([fed97db](https://github.com/rtcdance/streamgate/commit/fed97dbd2beb35a15b21a7e3eac11d33b82a7ede))
* **streaming:** publish transcodes by atomic generation switch (SG-12 step 8) ([62b93a0](https://github.com/rtcdance/streamgate/commit/62b93a0a5f735a64579657c364ea5a4e2b543dea))
* **streaming:** serve FFmpeg's real playlists with only URIs rewritten (SG-12 step 9) ([acf94f5](https://github.com/rtcdance/streamgate/commit/acf94f5878ebc743f2bd1a94c3574356a1a2045e))
* **transcode:** durable task authority, lease claim, outbox and DLQ (SG-12 steps 6+7) ([1ed9524](https://github.com/rtcdance/streamgate/commit/1ed9524e0fb3e722b277c12e5d18e50841e51e5e))
* **transcode:** probe gate for untrusted media and bounded workers (SG-12 steps 3+4) ([c640127](https://github.com/rtcdance/streamgate/commit/c6401275eb3bf039318ee832eee2e4983c4d36a0))
* **transcoder:** run media work in an isolated external worker (SG-12 step 5) ([85542c9](https://github.com/rtcdance/streamgate/commit/85542c9fe51df573aa318af3ad1c33c506f57f36))
* **upload:** seal originals with a server-computed digest and 409 conflicts (SG-12 step 1) ([7242290](https://github.com/rtcdance/streamgate/commit/724229038df32f2482c5b662bcca628df3a71b90))
* **verify:** a fresh-environment recovery drill as a real stage (batch C, part 3) ([3676814](https://github.com/rtcdance/streamgate/commit/3676814a93048c6e393a714cc6cb1f851525c6f4))
* **verify:** add VERIFY_PLATFORM knob so arm64 hosts can take the CI platform path ([e19beae](https://github.com/rtcdance/streamgate/commit/e19beaee3cee76cfc265a51d04cd10d66f934e8f))
* **verify:** frozen acceptance source, opt-in cache, real test execution ([55a0e18](https://github.com/rtcdance/streamgate/commit/55a0e18909d0a16a18952bc98257afe08ad9893d))
* **verify:** implement the five remaining verify-docker stages (SG-00/15) ([9144dd1](https://github.com/rtcdance/streamgate/commit/9144dd106cf1deff927fa701575c0aa5563ff88b))
* **verify:** qualify the runner, share the Go cache, lock the daemon ([de1ad69](https://github.com/rtcdance/streamgate/commit/de1ad698259a89fbaaa8874b642908ff7e5ab0bf))
* **verify:** rebuild the acceptance entry around a pinned controller (batch A) ([29f9645](https://github.com/rtcdance/streamgate/commit/29f96454b425b7aa059a7393b3ac017b601ffc4b))


### Bug Fixes

* **acceptance:** accept both container image identities for the public stack ([e8bf782](https://github.com/rtcdance/streamgate/commit/e8bf78241a5fa7b247c775328ae5dee2ac76491c))
* **acceptance:** actually drive the fault barrier and harden three scenarios ([d6e606c](https://github.com/rtcdance/streamgate/commit/d6e606c300d8debb3e831e841f1c08573edb70c2))
* **acceptance:** add the fuzz-target runner and its regression test ([850af43](https://github.com/rtcdance/streamgate/commit/850af431f2c3a532a336bb8d77a108d09aa50408))
* **acceptance:** arm the outbox windows concurrently and stop lying about them ([84fdba6](https://github.com/rtcdance/streamgate/commit/84fdba6961716c1b71b6df08fbb5c2faab2b44a0))
* **acceptance:** bind images by archive identity, not the local store ID ([e1520b5](https://github.com/rtcdance/streamgate/commit/e1520b5460ef749effdafaba68f564f2f5e65daf))
* **acceptance:** bind the response path before the array that uses it ([8ba5846](https://github.com/rtcdance/streamgate/commit/8ba5846b9b5d827e3d05c1115850e76162519ad9))
* **acceptance:** clean private backups when the controller is cancelled ([71ba0f7](https://github.com/rtcdance/streamgate/commit/71ba0f71e2c1a101568ede24bc04528be59a1b51))
* **acceptance:** correct the perf reference baseline to 71d23a1 ([5508bfd](https://github.com/rtcdance/streamgate/commit/5508bfdc67c8c12856e42c559ea4aec9e09d4f11))
* **acceptance:** define grpc_parity before the M3 revocation checks ([c9afd28](https://github.com/rtcdance/streamgate/commit/c9afd285a876b4cf0e05487b12b990b9aa38fa78))
* **acceptance:** derive the FI05 rewind landing from one head reading ([dab00c8](https://github.com/rtcdance/streamgate/commit/dab00c80bb7a049dde01e266a68fe2e0a97dd1d0))
* **acceptance:** do not let a Debian mirror outage sink a cached browser ([64722e0](https://github.com/rtcdance/streamgate/commit/64722e034f5e9a26edd554a465d032cfadd1d9bc))
* **acceptance:** dump the raw cast output when a mint yields no hash ([7f012d3](https://github.com/rtcdance/streamgate/commit/7f012d364a155e9da6ba53048295c6fe25caaae3))
* **acceptance:** enforce paced independent viewers and retain missing DLQ intent ([c04547e](https://github.com/rtcdance/streamgate/commit/c04547e4f7787eb2abf8fffce7eb5e8eb1f3b42f))
* **acceptance:** give a delegating stage its own completion record ([5d87fe8](https://github.com/rtcdance/streamgate/commit/5d87fe8b0d8859af73ca6aa625b1eef52723aa70))
* **acceptance:** hand the fault rendezvous to the worker's own user ([479affd](https://github.com/rtcdance/streamgate/commit/479affd843ec377f0993a15dcf065944f25d85a5))
* **acceptance:** keep the full scenario logs plus both endpoint proxies ([cee5498](https://github.com/rtcdance/streamgate/commit/cee5498477c60bf6c75217f980646c1846f9c386))
* **acceptance:** keep the monolith logs when a perf scenario fails ([2c7f22d](https://github.com/rtcdance/streamgate/commit/2c7f22d4ff400d2af484bfb2d9c7de624895ff7e))
* **acceptance:** log the cast failure on stderr with its parsed error ([a06bf34](https://github.com/rtcdance/streamgate/commit/a06bf34983eaf7977e3d154d155a2084bd33a355))
* **acceptance:** make candidate evidence portable across runners ([130715d](https://github.com/rtcdance/streamgate/commit/130715d1eceb2834b39247c3f00229fc4b8db4a5))
* **acceptance:** make clean-source checks portable across controllers ([26c68bf](https://github.com/rtcdance/streamgate/commit/26c68bfe85e846260051094ae169e028b1427923))
* **acceptance:** make the durable fault scenarios observe what they claim ([c4293a5](https://github.com/rtcdance/streamgate/commit/c4293a53ce5ca0eb02d51f57bf9540131fed5e99))
* **acceptance:** match the documented fuzz retry shape exactly ([224dd5e](https://github.com/rtcdance/streamgate/commit/224dd5ebafca6f98901c2f356044a2ec76ab6d5c))
* **acceptance:** match the registry fault barrier to its content intent ([7d84a57](https://github.com/rtcdance/streamgate/commit/7d84a57b6fe189d1c864246b91b6691f1c741927))
* **acceptance:** preserve original Sepolia wallet observations ([b376e02](https://github.com/rtcdance/streamgate/commit/b376e0271b1b86e45c41f8b56244b577718e1cbb))
* **acceptance:** preserve wallet evidence across lease rotations ([71b631e](https://github.com/rtcdance/streamgate/commit/71b631e72d5e69053cf62f8aadb0f39d309e450f))
* **acceptance:** read the stability window from its own statement ([e09c8b3](https://github.com/rtcdance/streamgate/commit/e09c8b3ef6f174d07e0cf2d338cdad8232ab2b6c))
* **acceptance:** record every completed stage marker for the evidence index ([8e403f4](https://github.com/rtcdance/streamgate/commit/8e403f4135f755fd3ead581b89974729feddd3ca))
* **acceptance:** refuse a foreign controller name before mutating ([2445d69](https://github.com/rtcdance/streamgate/commit/2445d69dafdbb2068f65dcd091c9d922e58e8df2))
* **acceptance:** reject every failed required fuzz run ([ef55299](https://github.com/rtcdance/streamgate/commit/ef5529963240145b9b747664ea3b8906d6491cf2))
* **acceptance:** reject unknown failures before retrying fuzz targets ([a3a7df3](https://github.com/rtcdance/streamgate/commit/a3a7df3db1c86d8df92ccbd2a847723e0f60d516))
* **acceptance:** repoint copied submodule gitdirs inside the reference worktree ([08ecaf1](https://github.com/rtcdance/streamgate/commit/08ecaf101e4d782a483be938d4662894ef9e1dd6))
* **acceptance:** resolve a relocated archive from its manifest directory ([101bd4a](https://github.com/rtcdance/streamgate/commit/101bd4a5f1af7d9563ec1821b199c0f444591a9f))
* **acceptance:** retry a fuzz deadline that produced no failing input, once ([6ffc2f9](https://github.com/rtcdance/streamgate/commit/6ffc2f96adbd10b943eb8ba75f1c701ac113183c))
* **acceptance:** retry the browser navigation once before failing playback ([6190ca9](https://github.com/rtcdance/streamgate/commit/6190ca9dd11caa5dd5356f1b4625c8918e88fd1d))
* **acceptance:** run browser playback against the nginx front door ([63c046c](https://github.com/rtcdance/streamgate/commit/63c046cbc11f33c6a6cfb2977666823f82b3d88b))
* **acceptance:** show the stack logs when the app services fail, and make FW05 a real fault ([82b8386](https://github.com/rtcdance/streamgate/commit/82b83864e7737ab0a68034c4364d68d679f553db))
* **acceptance:** surface restore-transfer failures before the manifest poll ([990efa2](https://github.com/rtcdance/streamgate/commit/990efa2117d764e41bb35090ffd48eb7daecd108))
* **acceptance:** surface the dev-mint HTTP status instead of collapsing it ([5c30956](https://github.com/rtcdance/streamgate/commit/5c309563af15ac54c9c6613644f6b4f68f94839e))
* **acceptance:** surface the real error when a chain_send mint fails ([4f9450e](https://github.com/rtcdance/streamgate/commit/4f9450e881df2c3101ae9d3786bd3a356b7e1465))
* **acceptance:** treat balance=0 as a failed mint ([6d90837](https://github.com/rtcdance/streamgate/commit/6d90837283ac2347608ff6ad4d982af6c0efb395))
* **acceptance:** treat release as delegated when stamping stage markers ([a63b15f](https://github.com/rtcdance/streamgate/commit/a63b15f5d8431e692b15b70887caac1f64e33994))
* **api-gateway:** wire the shared authorizer into the standalone gRPC server ([7a84ac1](https://github.com/rtcdance/streamgate/commit/7a84ac1e3e0af7a28f70b141f1774a9156923552))
* **auth:** apply the configured clock tolerance instead of a hardcoded 30s ([47ce7b0](https://github.com/rtcdance/streamgate/commit/47ce7b0637bf9dd5309345c960e5a31af6074937))
* **auth:** enforce consistent credentials and access keys across transports ([add9e6f](https://github.com/rtcdance/streamgate/commit/add9e6f2fdd5c18758e6c996f5f5c1e469440595))
* **auth:** logout revokes the whole session, not just the presented token ([098450b](https://github.com/rtcdance/streamgate/commit/098450b39ebb25fb69591d65d4600bf763fd8bd7))
* **auth:** pin policy snapshots and preserve NFT evidence age ([8c7c740](https://github.com/rtcdance/streamgate/commit/8c7c7403dbbb4a89acc306be2fc18d73be80105b))
* **auth:** refuse capability issuance after session or recovery changes ([9beccd5](https://github.com/rtcdance/streamgate/commit/9beccd5615d1db4b6020fe02b3c6c0ebf54f68e4))
* bind browser observations and image scans to candidate evidence ([fec06e4](https://github.com/rtcdance/streamgate/commit/fec06e44576a026b2a3ea8c304c38ea09981c841))
* **cd:** bind the transport consumer to the producing run ([ef57e92](https://github.com/rtcdance/streamgate/commit/ef57e92dfe37d4eeb4e3d698699f79398b8e67e3))
* **cd:** bound pull timeouts and retry resumable pull failures ([db29caa](https://github.com/rtcdance/streamgate/commit/db29caacffd85daccf7f9b13209502c55fac888e))
* **cd:** close the three transport P1s and anchor get to the declared root ([33a764e](https://github.com/rtcdance/streamgate/commit/33a764e9b2f687a4b6f45e6c34bdd0e43107d327))
* **cd:** give pulls the full registry budget back ([17c1416](https://github.com/rtcdance/streamgate/commit/17c14169c48ed319543e822ece3f8f2cf9282db1))
* **cd:** mirror upload-artifact selection and bind transport digests ([9e7e6e5](https://github.com/rtcdance/streamgate/commit/9e7e6e5fd0d6bdf193063026f9863e0c18e8c06c))
* **cd:** retry dropped registry requests in the transport helper ([f1be1b1](https://github.com/rtcdance/streamgate/commit/f1be1b165c92bed3929fe0675a10a15ac27276e4))
* **cd:** retry transient registry answers including GHCR blob 400 ([7b80166](https://github.com/rtcdance/streamgate/commit/7b8016641cadc40df8741dac39783509d12e38ed))
* **ci:** align evidence transport timeout cap with the workflow budget ([d151ae6](https://github.com/rtcdance/streamgate/commit/d151ae625999d5cec00720fe9bbb4ed3bcee8243))
* **ci:** bind complete candidate acceptance to original build evidence ([3d03845](https://github.com/rtcdance/streamgate/commit/3d038453aa84d927dedfc466e1398d5b00f8c448))
* **ci:** bind the smoke job to the archive identity across docker stores ([852ca5b](https://github.com/rtcdance/streamgate/commit/852ca5b77eef8bc38941a6b39416c91341625fd0))
* **ci:** bound registry transfers and retry transient failures only ([bfa816d](https://github.com/rtcdance/streamgate/commit/bfa816d039ccee445ca107f5a44f51dcbd67c413))
* **ci:** build contracts before the Go test gate so the ABI-sync guard can run ([7a9f274](https://github.com/rtcdance/streamgate/commit/7a9f2740dcef16db2e0cf627aa78ec06c828bf35))
* **ci:** build contracts on the test-unit-web3 leg so the ABI-sync guard runs ([954bd04](https://github.com/rtcdance/streamgate/commit/954bd045df0172b2f77a0349777174f5758bbcd7))
* **ci:** correct imagetools digest format in release-validation pin check ([41e7eac](https://github.com/rtcdance/streamgate/commit/41e7eac0d1cd7f049762465fb451cd97de0e3034))
* **ci:** give the acceptance a 900-minute clock and a durable browser cache ([25d36ec](https://github.com/rtcdance/streamgate/commit/25d36ec96482137539e9227d638c117e82c9dee8))
* **ci:** increase evidence transport timeout and retries for slow network ([65c09f3](https://github.com/rtcdance/streamgate/commit/65c09f32b21be9dda39e230029d8e36615eee08e))
* **ci:** install Foundry with the pinned action, not the moved installer ([3e3ed4c](https://github.com/rtcdance/streamgate/commit/3e3ed4c9460ed81ca830969ddab402309cac074d))
* **ci:** make deploy.yml credential gate and target resolution actually run ([e07f2be](https://github.com/rtcdance/streamgate/commit/e07f2be73f6d99cb4159361aacdf01b4a4af28eb))
* **ci:** persist self-hosted scanner database and use official registries ([dadb019](https://github.com/rtcdance/streamgate/commit/dadb019bd834ad6f28a2f2e15b5bcccee852d3ec))
* **ci:** pre-provision trivy for self-hosted scan jobs ([d0f0515](https://github.com/rtcdance/streamgate/commit/d0f05152edc1fb404cb2d78b734c95244e1a7a4e))
* **ci:** pull evidence images by their published platform on a foreign host ([037fc17](https://github.com/rtcdance/streamgate/commit/037fc17425c6b769577616c6a228b29dae04da1e))
* **ci:** replace the removed minio/minio image in the smoke and acceptance stack ([31f33b1](https://github.com/rtcdance/streamgate/commit/31f33b16f84668b7be79b5437cc4278ddacf8bce))
* **ci:** resolve acceptance modules through a reachable proxy ([cb01a0e](https://github.com/rtcdance/streamgate/commit/cb01a0ed5bbe41cf923b3c4ad7c2155f2e0a0d2f))
* **ci:** resolve all top-level Go module fetches through a reachable proxy ([b2af964](https://github.com/rtcdance/streamgate/commit/b2af964791b9dfc42ca9378e313400fc660bc812))
* **ci:** resolve digest-pinned base images through a reachable buildkit mirror ([44b860f](https://github.com/rtcdance/streamgate/commit/44b860f558cc56901e382967ffa303d5e2857aba))
* **ci:** retry completed registry response-header timeouts ([c38dc4b](https://github.com/rtcdance/streamgate/commit/c38dc4bc0bd5ac694261ee60ca07764783b1d05b))
* **ci:** retry the GHCR evidence-upload login ([76b7d40](https://github.com/rtcdance/streamgate/commit/76b7d40e8536249351815f5d07b9a376c729615e))
* **ci:** revalidate inherited publication receipts without relabelling artifacts ([caec879](https://github.com/rtcdance/streamgate/commit/caec87921e6141b44099e5e0848df18bbbeb56b5))
* **ci:** revert pull_image platform test to 3 calls ([9823995](https://github.com/rtcdance/streamgate/commit/9823995c51b98e14612febe2e1909c6ffdcbfce9))
* **ci:** route slither runtime pip through a reachable mirror ([de4ee34](https://github.com/rtcdance/streamgate/commit/de4ee347f829ca784db13a1b7e247743ce58b9d0))
* **ci:** run forge build inside contracts/ so it compiles the project sources ([73e894d](https://github.com/rtcdance/streamgate/commit/73e894dea9dea2fec841cc8d1a8a395e85fbc574))
* **ci:** serve the acceptance page from the nginx front door ([d81a440](https://github.com/rtcdance/streamgate/commit/d81a440ac7b68c052a65ce81e480ff6f0da117d8))
* **ci:** track the required-test assertion script the workflows invoke ([4fada9b](https://github.com/rtcdance/streamgate/commit/4fada9b15d96903e821c48f66b46f4347a49d145))
* **ci:** update evidence transport tests for 5-attempt retry budget ([88a863c](https://github.com/rtcdance/streamgate/commit/88a863cdf88c722a03da8235bf295ab76b5286c1))
* **ci:** write the migration ledger in the format the helper reads ([1422b59](https://github.com/rtcdance/streamgate/commit/1422b59bbb19fb9734ed991dab03d5217e4b6415))
* **compose:** replace the removed minio/minio image in the fullchain stack ([feae5b8](https://github.com/rtcdance/streamgate/commit/feae5b831eae560c548691d38be75840cbde3272))
* **config:** advance the safety baseline S to the first fully qualified revision ([a68d992](https://github.com/rtcdance/streamgate/commit/a68d992b4b81323897b7f649c678177d0d32e883))
* **config:** remap the safety baseline S onto the filter-repo rewritten history ([af00b8c](https://github.com/rtcdance/streamgate/commit/af00b8c2d6b8b23c26b837e91e8dff84796d6b21))
* **config:** remap the safety baseline S onto the re-rewritten history ([df253ef](https://github.com/rtcdance/streamgate/commit/df253ef0d9f83b377fc64131777b29dfe1a9fa34))
* **contracts:** make deployed bytecode reproducible and pin the demo-chain toolchain ([6242c9f](https://github.com/rtcdance/streamgate/commit/6242c9fc67b2915cb79bdb1054769b39b9aebcef))
* **deploy:** gate app startup on contract deployment, not anvil liveness ([a48d05d](https://github.com/rtcdance/streamgate/commit/a48d05d9e4f714f0dccdf4f63b206d03306a6d30))
* **deploy:** map host.docker.internal explicitly for Linux engines ([40e955a](https://github.com/rtcdance/streamgate/commit/40e955a406144c596935de1976b8bba78aaa118e))
* **deps:** drop the superseded compress 1.18.3 checksums ([7cb95c9](https://github.com/rtcdance/streamgate/commit/7cb95c9f4f90e8970e121c1e30fa307f77eb5464))
* **deps:** raise OpenTelemetry to v1.45.0 for GO-2026-6505 ([550dd36](https://github.com/rtcdance/streamgate/commit/550dd36518390b3b4811e81b48aff623ed529d84))
* **docker:** retry the Alpine package index fetch with a bounded backoff ([b6baf3f](https://github.com/rtcdance/streamgate/commit/b6baf3f41082a4a357829c736332f2ac152cad3d))
* **durability:** recover submitted transactions and invalidate NFT batches ([2919ebc](https://github.com/rtcdance/streamgate/commit/2919ebc43f4d53cfb164cde6bf2abe1e967e12fc))
* **durability:** repair the stale integration fixtures and serialize the shared DB ([162653b](https://github.com/rtcdance/streamgate/commit/162653b0c1737eb9c3b3c6035de0a07a78aa8c5a))
* **e2e:** drive the retry test through the worker's claim path ([45a3d02](https://github.com/rtcdance/streamgate/commit/45a3d023418e36bffcfaf40c5aaa1278860f2415))
* **e2e:** make the previously skipped acceptance paths execute ([b42a42a](https://github.com/rtcdance/streamgate/commit/b42a42a4e05d874e654c37671e4d554441116495))
* **e2e:** stop hiding schema failures behind a green job ([6629877](https://github.com/rtcdance/streamgate/commit/6629877ccd5987f875a0f1ea4e66c13a6e4969d7))
* **event:** commit the block journal atomically and verify the cursor anchor every poll ([7563f1b](https://github.com/rtcdance/streamgate/commit/7563f1bd87a6677ac16c38996dab5db6842f51be))
* **event:** end the poll at the anchor-triggered rollback so recovery stays observable ([82c3600](https://github.com/rtcdance/streamgate/commit/82c3600e16d32fd5d2379662ae99415eacfecd1a))
* **events:** anchor durable cursors and roll back reorg state atomically ([d167fa2](https://github.com/rtcdance/streamgate/commit/d167fa29d8482475124e9b15c253623e90975ee5))
* **events:** decode indexed logs and mark reorgs by block hash ([85cc4a7](https://github.com/rtcdance/streamgate/commit/85cc4a7f1d9bb4c621899674d4e92531085ae98a))
* **events:** persist the initial cursor before subscribing ([613c43e](https://github.com/rtcdance/streamgate/commit/613c43e8201a3f9dd754aac98733677384fd6cab))
* **faults:** log in again after the submitter restart ([6a30dc4](https://github.com/rtcdance/streamgate/commit/6a30dc411272d35d3e9513a195f5af51685756f9))
* **faults:** reuse one login and authenticate the MinIO delete ([2132800](https://github.com/rtcdance/streamgate/commit/21328003e8f45c9efc4ccc1d2db0898cf22d0d38))
* **gateway:** stop the IP-keyed global limiter from merging media viewers ([145ba0a](https://github.com/rtcdance/streamgate/commit/145ba0ac8869c8fde3cc45c34b4eef2fb3d49272))
* **gating:** a nested playlist re-checks the holding, not just the capability (SG-06 step 4) ([c74e8e0](https://github.com/rtcdance/streamgate/commit/c74e8e024d04eaa7b45c40be5bbf07e93b65eb60))
* **gating:** make the transfer-event cache invalidation reach what the gate wrote ([164986c](https://github.com/rtcdance/streamgate/commit/164986c9b0febe4b7348285d16b4ec58c4edf9a4))
* **gating:** report unverifiable NFT evidence as unavailable, not denied ([caeb1a4](https://github.com/rtcdance/streamgate/commit/caeb1a47f800b6f8ad99b0ee252a4d02cc2a8514))
* **h5-acceptance:** treat a self-healed /health 5xx as transient ([24f3c3a](https://github.com/rtcdance/streamgate/commit/24f3c3aca096f8902942998f19e5266469013d6f))
* **h5:** preflight the bundled browser and name the real launch root cause ([fabe20a](https://github.com/rtcdance/streamgate/commit/fabe20a2aaf6720f0f81fa32ba4c69c74f1b89f7))
* **lint:** clear every golangci-lint finding the unit gate surfaced ([8ea7f95](https://github.com/rtcdance/streamgate/commit/8ea7f95f1c55e2c9c87c11c089aa1749049d57dc))
* **lint:** clear the gocritic findings the unit gate rejects ([fdae840](https://github.com/rtcdance/streamgate/commit/fdae84096e59073e6b81672884202ee6304fe6fe))
* **metrics:** keep chain and profile values out of unbounded labels ([34c6e49](https://github.com/rtcdance/streamgate/commit/34c6e497c7feb05d9456c93e80301ea59194da2c))
* **monolith:** ship config files in the image like every other service ([4f3ebcc](https://github.com/rtcdance/streamgate/commit/4f3ebcc52806a1a46148ccce3b2f3e38a9dc325b))
* **nft:** reject empty ABI balances instead of reporting mint success ([5ceb1f5](https://github.com/rtcdance/streamgate/commit/5ceb1f5d59741d8b434d536236aad061b9a5ebf8))
* **ops:** drill flushed the wrong Redis db and assumed a quality name ([09e7471](https://github.com/rtcdance/streamgate/commit/09e7471ca0a1e89eb8e8d98eee28cf6bf6a3a7ab))
* **perf:** anchor sampling deadline to recorded start time ([5daf3c3](https://github.com/rtcdance/streamgate/commit/5daf3c3b4b3232b09d0cb18fccc2a8e3fad3e641))
* **perf:** compare background peers by composition, not by instance ID ([a023708](https://github.com/rtcdance/streamgate/commit/a023708a5e23c33ddf326df0ae8f6f97cc54cd3f))
* **perf:** enforce first-byte integrity and complete candidate evidence ([16443e5](https://github.com/rtcdance/streamgate/commit/16443e5b9aa308aa3a97a1776207be2e5fd18a42))
* **perf:** give one scenario a test budget that fits the paced login phase ([29dc81c](https://github.com/rtcdance/streamgate/commit/29dc81c512aa46a1a6c3ea7af7621f3c1f4e0e11))
* **perf:** give the SETUP phase the same test budget as the scenarios ([dfcc218](https://github.com/rtcdance/streamgate/commit/dfcc218b2b8438722f1fd08bc9f1d4864b376e13))
* **perf:** initialize standalone stage evidence directory ([9e14c44](https://github.com/rtcdance/streamgate/commit/9e14c44eedb3e8a42c8695fbe901a50eba2a62f5))
* **perf:** judge the 100-viewer tier of every ≥99% scenario as the capacity exploration ([fe3e77f](https://github.com/rtcdance/streamgate/commit/fe3e77f7d3caa23ab0cfb7c39cad000a033008d4))
* **perf:** keep the neighbour's compose project out of the compared identity ([0ee6123](https://github.com/rtcdance/streamgate/commit/0ee61235cf870ee24aee311a0e95f7461a7fb749))
* **perf:** measure the 100-viewer capacity tier once, not per repeat ([c7bb4f3](https://github.com/rtcdance/streamgate/commit/c7bb4f378a304600133e018e4f05d54b8afdbe19))
* **perf:** one tier rule for the Go harness and the Python protocol checker ([13e407c](https://github.com/rtcdance/streamgate/commit/13e407c7c6ba478d89ca067bf5589e76cfeef595))
* **perf:** require fixture media hashes and correct percentile provenance ([c4b4ccc](https://github.com/rtcdance/streamgate/commit/c4b4ccc22fe64fdac35b28ee783e0704ee03b6e8))
* **perf:** sample each resource role on its own schedule ([ef2adf7](https://github.com/rtcdance/streamgate/commit/ef2adf7acbfd7b826814ce05dc121d9721643095))
* **perf:** take the failover baseline from an API restarted at the sampling barrier ([6afeaac](https://github.com/rtcdance/streamgate/commit/6afeaac20a20342fe22d28634c3fa525f0959886))
* **perf:** take the sampled window from the sampler's monotonic clock ([35ece51](https://github.com/rtcdance/streamgate/commit/35ece5192f6547effa21f94851e8946f14853172))
* **perf:** verify real faults cold state and probe provenance ([5416165](https://github.com/rtcdance/streamgate/commit/5416165006f4cb97814a6054796934d11e75214a))
* **perf:** wait for the RPC proxies before sampling ([7e311a0](https://github.com/rtcdance/streamgate/commit/7e311a09191f1efde7830b4c17d3ff5193298812))
* **player:** treat 401/403 as authorization ended, not a retryable network error ([b3cde1f](https://github.com/rtcdance/streamgate/commit/b3cde1fde81cb321e84a916371c14f1d3896055e))
* **recovery:** decode pinned Foundry envelopes and require full seed ([1c7b90f](https://github.com/rtcdance/streamgate/commit/1c7b90fa11df69409999ed06c7bc241c10c4eb2f))
* **recovery:** match the target log in-process, not through a pipefail pipe ([43c7c1a](https://github.com/rtcdance/streamgate/commit/43c7c1ac0e5fa5080e8c6ea41842975296b47f6c))
* **recovery:** verify logical objects and protect restore snapshots ([e9cb409](https://github.com/rtcdance/streamgate/commit/e9cb4091435da9bbd7bce45116d91e8b73030908))
* **security:** build with Go 1.26.10 and compress 1.18.7 ([29d38cc](https://github.com/rtcdance/streamgate/commit/29d38cc5a2d5eea8c4662936420388b6c343b2c5))
* **security:** keep RPC endpoint credentials out of status and client logs ([76ec76e](https://github.com/rtcdance/streamgate/commit/76ec76eee1cfe17e2e3ccd9bd92c041beb742e91))
* **security:** patch musl-utils and golang.org/x/net CVEs ([66d7ea4](https://github.com/rtcdance/streamgate/commit/66d7ea4e5784da270d30083ee89a34b1e65b376f))
* **service:** confirm demo-mint receipts before reporting the balance ([2cee0da](https://github.com/rtcdance/streamgate/commit/2cee0da4cb94b3b47543220eade6dcc2f50e8cb5))
* **service:** refresh NFT evidence crossing the freshness bound during policy snapshot ([8132616](https://github.com/rtcdance/streamgate/commit/8132616cd2b90d9cdc21e089b82326f5eb33686b))
* **storage:** migrate under one shared lock and one migration set ([bcb5621](https://github.com/rtcdance/streamgate/commit/bcb562154387ed95ee082c6f37b3d33da4b1f40e))
* **streaming:** bind RPC media to published renditions and stored playlists ([55c14c7](https://github.com/rtcdance/streamgate/commit/55c14c7024d11c05f34a668bf20e54942fb62b2c))
* **streaming:** converge the streams schema with the service contract ([6e2a370](https://github.com/rtcdance/streamgate/commit/6e2a3709f8d7678e497d05f8a6ffcf8b0fea9f8d))
* **streaming:** refuse an unusable rendition label so segment keys stay inside the content ([bd5989b](https://github.com/rtcdance/streamgate/commit/bd5989bb52894df5e9762d05fa942ed522a09a5d))
* **test:** guard the mock block prover recorded calls ([d601b02](https://github.com/rtcdance/streamgate/commit/d601b028e792675f9a135bc3f6ec282542b976f1))
* **test:** insulate perf wall-clock windows from GC noise and serialize the gate ([790405e](https://github.com/rtcdance/streamgate/commit/790405e06f294a8ef36c1286e57ea76d861f7f77))
* **test:** judge the thin perf windows by the best of five attempts ([42bb99c](https://github.com/rtcdance/streamgate/commit/42bb99c88979784406a9b94d32f51e3e6002bfe8))
* **test:** make the e2e tokens and mocks match the real security semantics ([3081c52](https://github.com/rtcdance/streamgate/commit/3081c52f3391bc8e794dbe2200892bbae1e57b13))
* **test:** mint one absolute session deadline per synthetic session fixture ([d267ad4](https://github.com/rtcdance/streamgate/commit/d267ad4439ba672fecf373cae22f07852a0f6ada))
* **testnet:** serve the H5 panel modules as JavaScript ([685b6b9](https://github.com/rtcdance/streamgate/commit/685b6b9780daf03c4f3272728d08d6b1b169c16c))
* **testnet:** start and attest local Sepolia stack ([37027d1](https://github.com/rtcdance/streamgate/commit/37027d102ce90c72f01a46c1d8a2f7074864a230))
* **test:** sync the e2e mocks with the interfaces and vet e2e in the unit gate ([eba891a](https://github.com/rtcdance/streamgate/commit/eba891ae5709f95c9682fa0f67ad71b22795e22a))
* **transcode:** fetch only allowlisted sources; resolve hosts before trusting (SG-12 step 2) ([6bed3d3](https://github.com/rtcdance/streamgate/commit/6bed3d306ab170307c5304d88de15f5fa614828b))
* **transcoder:** bound health server header read time (gosec G112) ([9b4142b](https://github.com/rtcdance/streamgate/commit/9b4142b82f9dc5df33b2c3687590977421308ab4))
* **transcode:** refuse an oversized original instead of silently truncating it (SG-12 step 3) ([a375eee](https://github.com/rtcdance/streamgate/commit/a375eeeb18291f607253e99dce42047465997a47))
* **transcoder:** keep partial startup cleanup safe and repeatable ([71d23a1](https://github.com/rtcdance/streamgate/commit/71d23a17cce3b5a8407dcfaaf1c060a8ea4ce093))
* **transcoding:** create the queue consumer with an explicit ack policy ([97376d2](https://github.com/rtcdance/streamgate/commit/97376d262e0b96299d0f8851ee0ba10de3c4c42e))
* **transcoding:** hold the mutex across the Dequeue wakeup broadcast ([1aab0bd](https://github.com/rtcdance/streamgate/commit/1aab0bd9649f32ece522d9a02fd9e931b5cd5009))
* **transcoding:** serialize Dequeue wakeups to stop lost cond broadcasts ([7253e16](https://github.com/rtcdance/streamgate/commit/7253e16af618304c8e4372bc4c0b4a8d627509bc))
* **transcoding:** share durable outbox delivery settlement ([b75a2b2](https://github.com/rtcdance/streamgate/commit/b75a2b2b591448791bb91dc2d06013cf89d4fe90))
* **verify:** align machine inventory with Docker acceptance profiles ([f0de81c](https://github.com/rtcdance/streamgate/commit/f0de81ccb9b625881b8140aa846dbbab6aab5efb))
* **verify:** allow the local file transport when populating frozen submodules ([d9962d1](https://github.com/rtcdance/streamgate/commit/d9962d10c39a9197cd205919d3198afb524a7c60))
* **verify:** bake solc into the toolchain image, not every stage run ([e804967](https://github.com/rtcdance/streamgate/commit/e80496764bd84f662e911025c0d6d3bad544eba0))
* **verify:** close the anvil-tag drift and pre-check every build tag ([151df39](https://github.com/rtcdance/streamgate/commit/151df39093da917b47b2568dfe020c7369d984e2))
* **verify:** compare the candidate on the reference's wiring ([00994f1](https://github.com/rtcdance/streamgate/commit/00994f1e770a67e886435a198b5be9a04c947d7b))
* **verify:** fetch Go modules directly inside the verification image (SG-00) ([7a7c353](https://github.com/rtcdance/streamgate/commit/7a7c353c388972e5e08329a7a1b1c5e70228c42c))
* **verify:** follow the controller log and cancel without leaving resources ([d79efa9](https://github.com/rtcdance/streamgate/commit/d79efa9aa6fe0fc6afab3ac2f377883c89f9335e))
* **verify:** forward GOMAXPROCS and GOFLAGS into the controller ([539292a](https://github.com/rtcdance/streamgate/commit/539292ab18e1e65b751d270f78e5239b818f3944))
* **verify:** inventory the restored OBJECTS, not MinIO's write scratch ([88fba03](https://github.com/rtcdance/streamgate/commit/88fba0371b6150ede15a1c05953130be72528521))
* **verify:** keep runner preflight free of host Python ([a7791c1](https://github.com/rtcdance/streamgate/commit/a7791c1db985a629773dccae33a3115fa708da69))
* **verify:** keep the browser-only perf-seed tolerance size-independent ([7530e45](https://github.com/rtcdance/streamgate/commit/7530e45c6cbc5bb421a7dbcb2328a39ecee4dbe7))
* **verify:** keep the controller start tick real and keep its stderr ([ec72264](https://github.com/rtcdance/streamgate/commit/ec7226480311b0bee76451a87f2dff75c13f3396))
* **verify:** key the reference cache on the docker daemon, not the container host ([84f1578](https://github.com/rtcdance/streamgate/commit/84f1578e3a609bac273371c75e7317b217a363fa))
* **verify:** label the reference rounds and make the seed tolerance precise ([3ade03a](https://github.com/rtcdance/streamgate/commit/3ade03a0bd2215369be5ce5586d7a79234498eec))
* **verify:** let the declared harness apply to the directory being validated ([2e0faa9](https://github.com/rtcdance/streamgate/commit/2e0faa9278ea2313a5b79057a6a7662510ab0450))
* **verify:** let the Playwright browser download come from a mirror ([738d636](https://github.com/rtcdance/streamgate/commit/738d636499b4d62811d72637df5a10f60a851635))
* **verify:** make a browser failure visible instead of aborting the run ([902829e](https://github.com/rtcdance/streamgate/commit/902829ece7e632f2aa78c2f4af285226512b92da))
* **verify:** make acceptance cleanup idempotent and self-describing ([7220616](https://github.com/rtcdance/streamgate/commit/7220616d126a68e23950880f6659daa4b5128472))
* **verify:** make the Docker verification image actually runnable (SG-00) ([7c7879e](https://github.com/rtcdance/streamgate/commit/7c7879e47ef9ab660568dcfea95d4956556334cd))
* **verify:** make the golangci-lint install unable to cache a silent no-op ([0c4c9bc](https://github.com/rtcdance/streamgate/commit/0c4c9bc920ef95c518a2af1ca01cb61a32e27991))
* **verify:** make the H5 static dir traversable by the nginx worker ([3f32e42](https://github.com/rtcdance/streamgate/commit/3f32e428b1ba5ca7d4d424cfce9a08e7865f07a5))
* **verify:** make the nested frozen-source submodule URL rewrite effective ([c69ee3a](https://github.com/rtcdance/streamgate/commit/c69ee3a210afb9675c60a0e6dfb1e3a58c827515))
* **verify:** make the recovery drill's reconciliation and reachability real ([c08d92b](https://github.com/rtcdance/streamgate/commit/c08d92bd28f7d3131cac0442ce50e1149af3ae4d))
* **verify:** make the solc bake best-effort and skip it for host-side stages ([56a3fd6](https://github.com/rtcdance/streamgate/commit/56a3fd6cfa51d2abaee45b8b4d634dbc8ef16dd3))
* **verify:** move the solc prewarm above COPY, with a bounded timeout ([735a71a](https://github.com/rtcdance/streamgate/commit/735a71af65717625179879e291c4ae4b21e669a5))
* **verify:** parse controller before mutable checkout can change its exit ([a7c07e5](https://github.com/rtcdance/streamgate/commit/a7c07e5e3f4c51914832aa423fbde389b6b76ff2))
* **verify:** pass GOMAXPROCS/GOFLAGS into stage containers ([c5bbfd0](https://github.com/rtcdance/streamgate/commit/c5bbfd048cdc3872e300f072b2763d01dc1b7dfa))
* **verify:** point the driver at the frozen clone, not the editing tree ([df884f7](https://github.com/rtcdance/streamgate/commit/df884f7ce158658ae6fc07362626d91b19aeefc1))
* **verify:** point the host cross-build at VERIFY_GOPROXY ([4e38916](https://github.com/rtcdance/streamgate/commit/4e38916c518ade2e057dbe64b2fd4e3e69773b15))
* **verify:** populate nested frozen-source submodules offline ([2981dff](https://github.com/rtcdance/streamgate/commit/2981dff8a7b37672e2b45dd89622af69c763ff59))
* **verify:** readiness through the forwarder, and stop paying for the browser per run ([c3a0121](https://github.com/rtcdance/streamgate/commit/c3a0121475d4e493c7a91268f099eaebf4e27ec4))
* **verify:** reject stale or mixed-commit perf results ([0a63fdb](https://github.com/rtcdance/streamgate/commit/0a63fdb72c296a10ea3d116a0541331b62a6958f))
* **verify:** release the acceptance lock when an early check fails ([e669932](https://github.com/rtcdance/streamgate/commit/e669932ebe23816e5fd130ac7b8117d25a7807ee))
* **verify:** require clean source before terminal stage freezing ([1264317](https://github.com/rtcdance/streamgate/commit/1264317ac7eb9d05405375ad5c8e021f6862434e))
* **verify:** retain browser mount when enabling the Go cache ([7f2c208](https://github.com/rtcdance/streamgate/commit/7f2c208dea07ac0ade40e563dcf9abe1ba2f97aa))
* **verify:** retry the solc prewarm instead of failing on one dropped connection ([2ceb58e](https://github.com/rtcdance/streamgate/commit/2ceb58e3fd6aa3cfe470256754f290ed0456b735))
* **verify:** reuse the main tree's submodules in the reference worktree ([040938d](https://github.com/rtcdance/streamgate/commit/040938d12e237c81690718924f63a5af0616ca17))
* **verify:** run the anvil-tagged integration suites in the chain gate ([31efcd4](https://github.com/rtcdance/streamgate/commit/31efcd476709af8268db3b6258991ebbed13aa83))
* **verify:** run the cleanup trap on INT and TERM too ([4cd2507](https://github.com/rtcdance/streamgate/commit/4cd250798074c6e041acbbb7a866e20def6a589a))
* **verify:** run the lint install step under bash (dash rejects pipefail) ([afc74d4](https://github.com/rtcdance/streamgate/commit/afc74d401cb466e8c3ced6a1cd1139be9215812c))
* **verify:** select the h5 directory deterministically in the harness test ([6b613ec](https://github.com/rtcdance/streamgate/commit/6b613ec04a25b1562f797cbdfb9c8f0c0b5950c0))
* **verify:** send fault-suite mints from a dedicated harness account ([ac54e29](https://github.com/rtcdance/streamgate/commit/ac54e298e86f4559413f9ad13a41cace8ac7f570))
* **verify:** serialize the load-sensitive timing tests in the unit gate ([1e1e9c4](https://github.com/rtcdance/streamgate/commit/1e1e9c4920dc83660ccac6ad7796f8d8b18938af))
* **verify:** share compilation and browser caches with the controller ([8a67064](https://github.com/rtcdance/streamgate/commit/8a67064b74cf1bdc7ce632bf8cca90a1e4a30aa3))
* **verify:** ship the deployment record in the verification runtime image ([8b48fe7](https://github.com/rtcdance/streamgate/commit/8b48fe70171652c192910e88e1c2f057227347f6))
* **verify:** source Foundry from pinned ghcr image instead of GitHub release assets ([abe2b13](https://github.com/rtcdance/streamgate/commit/abe2b13183d6f720caae51e0417f78e34263427c))
* **verify:** stamp the measured perf matrix with the candidate revision ([4ab3ea2](https://github.com/rtcdance/streamgate/commit/4ab3ea26060114d16352da68a42bd88ec080dc0a))
* **verify:** support scoped full acceptance on Mac Docker Desktop ([a4d6faf](https://github.com/rtcdance/streamgate/commit/a4d6faf47eb2e23d714a3f9328a568c065ab4863))
* **verify:** tear down each fullstack stage so `all` can actually chain ([0f18cdf](https://github.com/rtcdance/streamgate/commit/0f18cdf5eb48aed12bce071d11e2ca556f36a2fd))
* **verify:** tear down recovery proxies and networks before later stages ([9b8f0d1](https://github.com/rtcdance/streamgate/commit/9b8f0d14f37a52d288481caaab371e15ff120769))
* **verify:** tolerate a browser-only failure in the reference seed ([443c1ab](https://github.com/rtcdance/streamgate/commit/443c1abd6933bce7188795cc595021be66c247d8))
* **verify:** wait for controller startup before following its logs ([283bdda](https://github.com/rtcdance/streamgate/commit/283bdda9db040e3dce407d318dbcd6ea465b2d31))
* **web3:** accept the documented 0x prefix on the configured signing key ([8894658](https://github.com/rtcdance/streamgate/commit/88946581a99f9dc8edb636f07a382427bccbd00a))
* **web3:** bound concurrent RPC calls so a viewer burst stops queueing ([4056698](https://github.com/rtcdance/streamgate/commit/405669831a7fd5026627d9310d22fd1b920f0734))
* **web3:** bound one RPC request so a hung endpoint cannot starve the failover ([2f5648c](https://github.com/rtcdance/streamgate/commit/2f5648c6ed83205ef8013005f7e9b7c4cbc0d2ec))
* **web3:** charge an RPC failure to the endpoint that served it ([ecdcc04](https://github.com/rtcdance/streamgate/commit/ecdcc049888d1f78506f2f0cb785e37eb0f0587f))
* **web3:** floor every nonce allocation with the chain pending nonce (SG-13 A.3 follow-up) ([89d3ad7](https://github.com/rtcdance/streamgate/commit/89d3ad777c525906c88caa9ea66afcb9a5604312))
* **web3:** make the DB ledger the single nonce authority (SG-13 A.3) ([1b35e1b](https://github.com/rtcdance/streamgate/commit/1b35e1bae4ca4f0eb309af0dd37e60ce001d59e4))
* **web3:** share an enlarged RPC idle pool across chain calls ([c3fb9cc](https://github.com/rtcdance/streamgate/commit/c3fb9cc023139a5e3ec6ee8670ca03df64e64a88))
* **worker:** don't let the job queue resurrect a cancelled job ([871eab3](https://github.com/rtcdance/streamgate/commit/871eab307a2ddd332301d8187566241dc7a2e151))


### Performance Improvements

* **acceptance:** reuse the verified Go compile cache in CI ([4832c60](https://github.com/rtcdance/streamgate/commit/4832c60fa5c03ee0de839c371b83c6c15a5d83bb))
* **ci:** scope the buildx cache per image and export only the final stage ([479ebbd](https://github.com/rtcdance/streamgate/commit/479ebbd28ca9e496818be3d6b462da351f2b7fc7))
* **verify:** reuse a validated reference measurement on the same runner ([7263110](https://github.com/rtcdance/streamgate/commit/72631105d0eab27bdf663502dcdaaa9845729321))

## [1.0.6] — 2026-10-11

### Security

- **Authorization bypass closed.** A forged `playback_token` query parameter used to skip JWT
  validation and reach a playback-token mint for an empty wallet. `PlaybackTokenAuth` now
  cryptographically validates the token before setting `playback_auth` — the parameter's mere
  presence grants nothing. Pinned by `pkg/gateway/authz_regression_test.go` (10 tests).
- **The NFT gate fails closed.** No rules → `403 GATING_REQUIRED`; rule-resolution error → `500`;
  gate disabled → `503`. It previously failed open.
- **Authorization subject is server-side only.** The contract / tokenId / chain to verify against
  now comes exclusively from `content_gating_rules`; the caller-supplied `?contract=` override
  (HTTP) and the equivalent gRPC metadata override were removed.
- `ValidatePlaybackToken` now requires an explicit wallet — the variadic form let callers silently
  skip the wallet check.

### Changed

- Go toolchain 1.25 → **1.26** (`toolchain go1.26.9`); dependency set aligned with the sibling
  `chainpulse` module where compatible. See `DEPENDENCY_APPROVAL.md`.
- Security bumps: `grpc` → v1.83.2, `golang.org/x/net` → v0.60.0, `golang.org/x/crypto` → v0.57.0.
- Removed the unreferenced `NFTGate` / `StreamNFT` contracts; the on-chain content registry
  (`ContentRegistry`) is now wired into the content service at runtime instead of being dead code.
- Deleted dead code and coverage-padding tests (~1,800 lines); the CI coverage floor was raised
  from 10% to 60%.

### Added

- `make h5-demo-acceptance` — Playwright end-to-end suite (5 specs + stack pre-check), including a
  probe asserting an unauthenticated manifest request returns `401`.
- `make fullchain-deploy` now deploys `ContentRegistry` and seeds a server-side demo gating rule.

## [1.0.0] — 2026-05-19

StreamGate v1.0.0 marks the first stable release after 20 rounds of architecture hardening, security review, and production readiness optimization. All P0 core features are stable and tested.

### For Platform Developers

- **Wallet Sign-In**: EIP-191, EIP-712, SIWE (EIP-4361), and Solana ed25519 — verified and hardened
- **NFT Verification**: ERC-721/1155 with ERC-165 auto-detection, CheckApproval for TOCTOU protection
- **Solana Multi-endpoint Failover**: Added `SolanaMultiClient` with score-based failover (matching EVM ChainClient)
- **JWT Key Rotation**: Comma-separated `AUTH_JWT_SECRETS` — first signs, rest verify
- **Per-Wallet Rate Limiting**: `WalletRateLimiter` — 10 req/min default, IP fallback
- **gRPC Standard Health Protocol**: `grpc_health_v1` for Kubernetes readiness probes
- **gRPC TLS**: Certificate-based server-side TLS via config
- **OpenAPI 3.0 Spec**: Complete 1105-line specification at `docs/api/openapi.yaml`

### For Content Creators

- **One-Command Demo**: `make demo` — auto-starts infra, builds, and runs
- **Demo Mode**: `STREAMGATE_DEMO_MODE=true` for blockchain-free testing
- **Chunked Upload**: Resumable with integrity verification
- **HLS Streaming**: Per-user playback tokens (2min TTL, bound to wallet+content+contract)
- **FFmpeg Transcoding**: Multi-profile (1080p/720p/480p/360p) HLS + DASH

### For Node Operators

- **Graceful Shutdown**: Context-aware with 30s global timeout, per-operation timeout
- **Configuration Hot Reload**: Viper-based with thread-safe update, change handlers
- **PostgreSQL Connection Pool**: Configurable with circuit breaker protection
- **EventBus NATS Fallback**: Degrades to MemoryEventBus on NATS failure
- **Database Migrations**: 31 versioned SQL files with rollback and dirty detection
- **CI/CD Pipeline**: GitHub Actions — lint → vet → test → build → security scan → Docker
- **Distroless Docker Image**: `docker build --target runtime-distroless .`
- **Prometheus RED Metrics**: Rate/Errors/Duration with service-level labels
- **OpenTelemetry Tracing**: Gin + gRPC integrated, export to Jaeger

### Security Hardening (20 rounds)

- Configuration redaction for all sensitive fields (DB password, private key, storage keys)
- ERC-1155 approval check added to existing ERC-721 check
- PostgresDB context cancellation bug fixed (rows returned with canceled context)
- K8s Secret template: envsubst required, no literal `${VAR}` in cluster
- CORS wildcard replaced with specific origins in dev/test configs
- Challenge ID UUID format validation
- `X-Content-Type-Options: nosniff` added to HLS manifest endpoint
- Transcoding queue errors logged instead of silently dropped
- ConfigManager deadlock fixed (handlers called outside write lock)
- MockAuthStorage data race fixed (added sync.Mutex)
- Migration 028 FK type mismatch fixed (VARCHAR↔UUID incompatibility)
- ContentRegistry smart contract registerContent() → onlyOwner

### Infrastructure

- Docker Compose services now have `restart: unless-stopped`
- NATS healthcheck improved (TCP port check vs binary existence check)
- Jaeger image pinned to 1.57.0 (was latest)
- Grafana/MinIO default passwords removed (must set via env)
- Request ID propagation across HTTP→gRPC boundaries
- `build-all` Makefile target includes `build-learn`

### Testing

- All 18 Go packages pass tests with race detection enabled
- Go vet: zero warnings across entire codebase
- Benchmarks: all 36 functions execute (was 58% skipped)
- WebSocket auto-reconnect tested with exponential backoff (1s→30s)

### Documentation

- Web3 glossary: 11 terms defined in README
- User personas: Platform Developer / Content Creator / Node Operator
- Product roadmap: P0 Core / P1 Usability / P2 Ecosystem

## [0.10.0] — 2025-05-16

> Renumbered from a duplicate `[1.0.0]` heading: this entry predates `1.0.0` (2026-05-19) and
> follows `0.9.0` (2025-04-01), so it was mislabelled.

### Added
- Request trace visualizer — middleware chain animation, flame graph, code path display
- H5 debug-learn page with breakpoint hints and code path traces
- Learner tools suite: comparison guide, chaos demo, interactive playground
- Web3 state debug endpoint (`/debug/web3-state`)
- 10 concept demonstration tests in `test/demo/concepts_test.go`
- Full-chain acceptance flow with Docker Compose
- NATSEventBus real NATS implementation
- Progressive NFT verification tutorial (3-stage demo)

### Changed
- Transitioned from microkernel skeleton to service-based architecture
- Migrated multi-Dockerfile layout to monolith-first deployment
- Aligned all builds and CI to Go 1.24

### Fixed
- NATS event bus skeleton replaced with real implementation
- Acceptance documentation aligned with Docker verification path
- Upload security hardening and storage streaming

## [0.9.0] — 2025-04-01

### Added
- NFT-gated HLS streaming with multi-bitrate support
- Wallet sign-in with EIP-191/712/SIWE and Solana ed25519
- NFT verification with ERC-721/1155, ERC-165 detection
- Reorg protection with safe/finalized block tags
- Event indexing subsystem for blockchain events
- Transcoding pipeline with FFmpeg
- Multi-chain support (EVM + Solana) with RPC failover
- JWT-based session management with RS256 signing
- Circuit breaker middleware for external calls
- Rate limiting middleware (initial implementation)

### Infrastructure
- Docker Compose stack: PostgreSQL 15, Redis 7, MinIO, NATS
- Kubernetes manifests with External Secrets Operator
- Prometheus + Grafana observability stack with pre-built dashboards
- Jaeger distributed tracing via OpenTelemetry
- GitHub Actions CI/CD with lint, test, build, security scanning
- Canary and blue-green deployment scripts

### Testing
- 132 test files: unit, integration, e2e, benchmark, load, security
- Foundry Anvil-based Web3 integration tests
- 40% coverage floor enforced in CI
- Load tests sustaining 5000+ req/s at 200 concurrency

### Security
- gosec + slither + Trivy in CI pipeline
- External Secrets Operator for K8s secrets management
- Formal dependency approval process (`DEPENDENCY_APPROVAL.md`)
- RBAC with least privilege on K8s service accounts
- AES-256-GCM encryption utilities
