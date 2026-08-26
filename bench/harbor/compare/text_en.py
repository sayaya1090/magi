"""English prose for the comparison page.

The page's numbers come from the run; its sentences come from here. Keeping them apart is what
lets the same builder emit an English page and a Korean one from one set of measurements -- and
what stops a translation drifting away from the figure it describes. Keys match text_ko.py.
"""

# What the failure actually was, where a run has been read line by line. Anything not named here
# is described from its own record instead of guessed at.
NOTES = {
 "path-tracing-reverse": ("Spent the whole thirty minutes analysing and <b>never wrote mystery.c "
   "once</b>. It meant to write after working everything out, and the clock took the file with it."),
 "gcode-to-text": ("<b>Read an M486 object's label as its contents.</b> It doubted itself enough to "
   "search twice, and adopted the wrong reading anyway. Council 3-0 done."),
 "path-tracing": ("Zero on a clean exit. Not the clock — it <b>declared done on a wrong artifact</b>."),
 "qemu-alpine-ssh": ("<b>15 minutes, 1 CPU, 4 GB, no KVM.</b> The container is amd64 and the host is "
   "aarch64, so <code>qemu-system-x86_64</code> core-dumped on startup with "
   "<code>rosetta error: Unimplemented syscall number 282</code> — the wall this repository's own "
   "notes had called unpassable here. This trial went over it <b>without a single web call</b>. It "
   "first tried an arm64-native QEMU via <code>dpkg --add-architecture arm64</code>, and "
   "<b>that route failed</b> on gstreamer dependencies. So it went a layer lower: installed gcc and "
   "binutils, located <code>qemu_signalfd</code> with <code>objdump -T</code>, disassembled "
   "0x8bc6e0, and <b>wrote a 32-line LD_PRELOAD interposer</b> that returns <code>ENOSYS</code> for "
   "syscalls 282 (signalfd) and 289 (signalfd4) so QEMU falls back to its own pipe-based path. The "
   "first compile broke on <code>NULL undeclared</code> and <code>&lt;stddef.h&gt;</code> fixed it. "
   "Then SeaBIOS came up.<br><br>The rest of the budget went to the serial console. With no kernel "
   "log visible it tried to mount the ISO, hit a permissions wall, installed "
   "<code>genisoimage</code>, pulled <code>/boot/syslinux/syslinux.cfg</code> out with "
   "<code>isoinfo</code> and confirmed there was no <code>console=ttyS0</code>. Typing at the boot "
   "prompt lost to a short <code>TIMEOUT</code>, so it extracted vmlinuz and initramfs from the ISO "
   "and booted them directly with <code>-kernel</code>/<code>-initrd</code>, skipping isolinux. "
   "Alpine 3.19 reached <code>localhost login:</code> after <b>3 m 2 s</b> of VM time. Moments after "
   "it sent <code>root</code> the fifteen minutes were gone, sshd untouched, and the council never "
   "convened — the harness cut this run, it did not declare itself done. "
   "<b>What stopped it was the budget, not the method.</b>"),
 "feal-linear-cryptanalysis": "Waited out thirty minutes in a single <code>wait_for</code>. No artifact.",
 "filter-js-from-html": ("Wrote a regex sanitiser and verified it <b>only against attacks it invented "
   "itself</b>. It was diligent about those — its own tests caught a bracket-balance bug and an "
   "<code>srcdoc</code> ordering bug, and it fixed both — but everything it blocked was something it "
   "had thought of. The grader runs vectors through a real browser under selenium. Council 3-0 done: "
   "the evidence was concrete, and nothing in it argued against the inputs having been self-chosen."),
 "raman-fitting": ("The council <b>named the consistency trap exactly</b> — both peaks off by the same "
   "2.37× — but the calibration check it asked for cannot be satisfied in this sandbox."),
 "video-processing": ("The hurdle-jump heuristic never converged. The turn ended while it was "
   "redesigning candidate frames around valley prominence, and it <b>did not declare done</b> — it "
   "knew the output was wrong, so the run landed UNVERIFIED and no council opened. "
   "<code>jump_analyzer.py</code> is standing."),
 "sam-cell-seg": ("The MobileSAM converter was built properly — one embedding reused, box plus "
   "centre-point prompts, overlap removal, largest connected component kept, simplify then "
   "re-rasterise. It converted all 48 demo shapes (19 rect, 29 polyline) and verified zero overlaps "
   "and zero multi-component masks. Then, <b>tidying up, it deleted <code>/app/mobile_sam.pt</code></b> "
   "— the exact file the grader passes as <code>--weights_path /app/mobile_sam.pt</code>. It had "
   "written down that \"the hidden test will pass its own path\", which was a guess. Council 3-0 done."),
 "openssl-selfsigned-cert": ("<b>Five of six checks passed</b> — directory, key at 600, certificate, "
   "PEM, verification.txt, all correct. One failed: it tested <code>check_cert.py</code> under "
   "<code>python3</code> while the grader calls <code>python</code>, and those are different "
   "interpreters here, so the grader died on "
   "<code>ModuleNotFoundError: No module named 'cryptography'</code>. The instruction never says "
   "which name will be used, so there was nothing to check against — but running it both ways was "
   "available. Same family as <code>sam-cell-seg</code>: <b>an assumption about how the grader "
   "would invoke things</b>, and it was wrong. Council 3-0 done."),
 "build-pov-ray": ("<b>This is where the quarantine changed a score.</b> The first trial (08-25) read "
   "the dataset's <code>solution/solve.sh</code> and passed, which is why it was held. The re-run "
   "never touched the dataset — it searched out a mirror, built POV-Ray 2.2 itself, stood up "
   "<code>/usr/local/bin/povray</code>, rendered <code>illum1.pov</code> and confirmed a byte-exact "
   "30,018-byte TGA. Two of three checks passed: <b>the render match and the version check</b>. The "
   "one that failed is <code>test_povray_built_from_correct_source</code> — no <code>file_id.diz</code> "
   "in the source tree, a file that ships only in <b>the distribution archive the solution names</b>. "
   "It fetched the same version in different packaging. Claude Code is 0/5 here too."),
 "extract-moves-from-video": ("Transcribe the commands typed in a video of someone playing zork. "
   "Thirty-minute budget, one core. With no ffmpeg it installed one, extracted at <code>fps=2</code> "
   "and got 380 frames — commands sit on screen for seconds, so <code>fps=1</code> or lower would "
   "have done. It ran tesseract with <code>-P 4</code>, which buys nothing on one core: at frame 148 "
   "it was managing eight per thirty seconds, leaving fifteen more minutes for the remaining 232. "
   "Not an environment problem — one core and 2 GB are what the task declares, and host load was "
   "2.12. CC is 1/5, and one of its failures ran 31 m 13 s against this run's 31 m 43 s: the same wall."),
 "make-doom-for-mips": ("Build doom freestanding for MIPS without a cross toolchain and produce an "
   "ELF. Twenty-five <code>write</code> calls inside a fifteen-minute budget, most of them "
   "<b>hand-filling libc headers that are not there</b> — the last action is writing "
   "<code>CHAR_BIT</code> and the rest into <code>/app/build/inc/limits.h</code> and starting another "
   "build. Filling one header surfaces the next compile error, which is not an end reachable in "
   "fifteen minutes. <b>CC is 0/5</b> and all five of its trials were cut around sixteen minutes — "
   "the same wall this run (16 m 34 s) hit."),
 "install-windows-3.11": ("The instruction pins <i>\"known to be compatible with QEMU 5.2.0\"</i>, so "
   "when the container's version differed it decided to <b>build QEMU from source</b>. The last two "
   "calls are that build: 905 of 2289 objects at eight minutes, <i>\"steady but slow, continuing to "
   "wait\"</i>. Neither agent is told its budget — the leaderboard's Claude Code has no time in its "
   "prompt either — so the fault is not that it did not know the clock but <b>how it waited</b>: "
   "having used <code>bash_output</code> twice, it fell back to repeating <code>sleep 180</code>. A "
   "<code>wait_for</code> on a condition would have handed back the moment the build finished; "
   "instead it slept through it. It declared nothing, and landed UNVERIFIED."),
 "sanitize-git-repo": ("<b>Both the secret removal and the replacement passed</b>, two of six checks. "
   "The one that failed is <code>test_no_other_files_changed</code>, where the grader went looking for "
   "the original commit and died on <code>SHA d6987af0… missing</code> — <code>git-filter-repo</code> "
   "had rewritten the whole history and every hash with it. And <b>magi's own irreversible-command "
   "gate stopped it twice</b>: once on <code>git push --force</code> (right) and once on deleting the "
   "<code>/app/dclm_backup_…</code> it had made for safety (wrong — that backup still held the secret "
   "it was told to remove). The workspace had narrowed to the git root <code>/app/dclm</code>, which "
   "put the sibling path \"outside\". The council rejected four times and passed on the fifth, and "
   "that round trip ran past the fifteen-minute budget."),
 "gpt2-codegolf": ("<b>Wrote a working GPT-2 inference stack from scratch</b> inside a fifteen-minute "
   "budget — validated a BPE tokeniser against published ids (<code>\"Hello, my name is\"</code> → "
   "15496 11 616 1438 318), hypothesised the checkpoint's tensor order and disproved it (the natural "
   "order collapsed the output; TensorFlow's alphabetical order was right), and got as far as "
   "<code>\"Hello, my name is John. I'm a writer, and I'm\"</code>. Only the size constraint beat it: "
   "it was down from 7,648 bytes to 5,835 when the clock cut in, against a target of 5,000."),
 "train-fasttext": ("62 calls, cut at a full hour. The training itself worked, through three changes of "
   "direction — it noticed that autotune (5 trials, 28 MB, accuracy 0.609) had <b>read the size "
   "constraint as an instruction to shrink</b> and thrown capacity away, hand-tuned from there, and "
   "reproduced fastText's own yelp hyperparameters from the upstream repository. What stopped it was "
   "not training but <b>verification</b>: under numpy 2.x the fastText binding's <code>predict()</code> "
   "blows up on <code>np.array(probs, copy=False)</code>. Writing that cause into "
   "<code>remember</code> was its last action."),
 "db-wal-recovery": ("Passed in <b>7 calls, 3 m 17 s, $0.29</b>. It read the WAL frames directly, found "
   "the recovery path and did not wander. Claude Code failed all five trials, at $0.53 each."),
 "regex-chess": ("<b>The grader returned 1.00, and the leaderboard's own rule rescores it to zero.</b> "
   "This trial fetched "
   "<code>raw.githubusercontent.com/carlini/regex-chess/main/chess_engine.py</code> and "
   "<code>write_regex_json.py</code> — the task author's own published implementation. tbench.ai's "
   "leaderboard integrity policy defines reward hacking as <i>resolving a task without demonstrating "
   "the capability it was meant to measure</i>, sets the consequence at <b>a reward of zero for the "
   "trial</b>, and gives finding solutions on the internet as the example (precedent: ForgeCode's "
   "trials, which curled the solution from the web, rescored to zero). Eight attempts on 2026-08-26 "
   "all took the same path — read <code>check.py</code>, search, find a mirror or the official "
   "repository, open <code>solution/</code>, take the raw file. Four different mirrors, and one "
   "attempt thought for eleven minutes before going. <b>Not one attempt decided to write it itself.</b> "
   "Four of the baseline's five trials went nowhere at all and passed."),
}

# Which web calls each counted run made, and what it was actually after. Read, not summarised.
WEBWHAT = {
 "caffe-cifar-10": "another mirror for <code>cifar-10-binary.tar.gz</code> — where to download it",
 "compile-compcert": "<code>compcert.org</code>'s download and release pages — where the source tarball is",
 "build-pov-ray": ("where to get the POV-Ray 2.2 source archive — two searches, two fetches (an ibiblio "
   "index and a third party's notes on this task). It was looking for a download, and never reached "
   "the dataset. <b>And it still failed, because the archive it found was not the one the solution names</b>"),
 "count-dataset-tokens": "the card and README of the HuggingFace dataset it was asked to count",
 "dna-assembly": "NEB's BsaI-HF v2 guide — the extra bases needed when a cut site sits near a fragment's end",
 "break-filter-js-from-html": "tags BeautifulSoup's <code>html.parser</code> misses and a browser executes",
 "gcode-to-text": "PrusaSlicer's default M486 object names — <b>it looked, and still got it wrong</b>",
 "train-fasttext": "fastText's own <code>classification-results.sh</code> — the yelp hyperparameters",
 "protein-assembly": ("searched the 38-character light-chain sequence it had read out of "
   "<code>antibody.fasta</code> to identify the antibody — recognising data it was given, not looking "
   "up an answer"),
 "mteb-leaderboard": ("<b>the dataset's own task README</b> — and on all five trials this repository has "
   "watched. The instruction asks for <i>the best embedding model on the Scandinavian MTEB leaderboard "
   "as of August 2025</i>, which cannot be answered without the web, and that search puts the task's "
   "own page near the top. Quarantine → re-run → reach again repeats without end, so this row carries "
   "its re-run result and states the reason here (same clause in <code>docs/BENCHMARK.md</code>). "
   "<b>Do not weigh this row like the others</b>"),
 "mailman": ("Mailman3's own MTA integration page (<code>docs.mailman3.org .../mta.html</code>) — how "
   "postfix's <code>local_recipient_maps</code> and <code>transport_maps</code> line up on one domain. "
   "A tool's documentation about its own configuration"),
 "qemu-startup": ("the error string it had just been handed, <code>rosetta error: unimplemented syscall "
   "282</code> — the problem x86_64 QEMU hits under Rosetta. It found the way around and passed"),
 "torch-pipeline-parallelism": ("searched <code>train_step_pipeline_afab</code>, the function the task "
   "<b>names outright</b>, and read huggingface/picotron_tutorial's reference implementation. Not the "
   "dataset and not a solution file, but a public tutorial that is <b>this task's origin</b>. It also "
   "fetched the registry's task page once, which is an SPA and returned a shell. The first trial was "
   "held for the same reason and the re-run walked the same road, so like "
   "<code>mteb-leaderboard</code> this row carries its re-run result with the reason stated here. The "
   "code it wrote is its own, built on <code>_partition_layers</code> and "
   "<code>batch_isend_irecv</code>, not copied. <b>Do not weigh this row like the others</b>"),
 "regex-chess": ("<b>the answer file itself</b> — <code>chess_engine.py</code> from "
   "<code>carlini/regex-chess</code>. Under the leaderboard's rule this trial is <b>rescored to "
   "zero</b>; the account is on its card"),
}

# Trials rescored to zero under tbench.ai's leaderboard integrity policy. See text_ko.py for the rule.
REWARD_HACK = {
    "regex-chess": "fetched chess_engine.py from carlini/regex-chess and solved from it",
}

# The sentences the builder assembles rather than looks up.
UI = {
    "arc_missing": "no archive",
    "webwhat_missing": "<b>not yet read for what it was after</b>",
    "cc_also": " <span class='tag'>CC too, %d/5</span>",
    "only_pass": ("passed in %d calls, %d council rounds, %d m %d s, $%.2f. "
                  "Claude Code failed all five."),
    "why_unverified": ("landed UNVERIFIED <b>without declaring done</b> — it knew its own result was "
                       "wrong, so no council opened"),
    "why_timeout": "cut by the wall clock",
    "why_wrong": "zero on a clean exit — it declared done on a wrong artifact",
    "fail_tail": ("{why}. {calls} calls, {cn} council rounds, {mins} m {secs} s. "
                  "<b>This run has not been read yet.</b>"),
    "quarantine": ('<li><b>A trial that went looking for the answer is held and re-run.</b> A trial '
                   'whose web calls reached the dataset\'s own task or solution pages stands in no '
                   'table — a verdict reached after finding the answer is not evidence about the '
                   'agent. Waiting on a re-run right now: %s. The re-run is the result that counts.</li>'),
    "web_head": ('<li><b>Both agents used the web — this is not one-sided.</b> magi has '
                 '<code>websearch</code> and <code>webfetch</code> as tools; Claude Code has neither, '
                 'and has Bash instead. Pulling all 439 trajectories from the leaderboard job and '
                 'counting: <b>54 trials (22 tasks) called curl or wget against an external host</b>, '
                 '30 of them (13 tasks) somewhere that was not a package or distribution mirror. Six '
                 'trials across four tasks called a search engine directly — no search tool, but '
                 'search all the same. Of the %d tasks counted here magi used the web on %d, and on '
                 '%d of those the baseline went outside for the same task. Here is what it was after:'),
    "web_tail": ('<div style="margin-top:8px">Where both went out, <b>they often went to the same '
                 'place</b> — <code>dna-assembly</code> to <code>www.neb.com</code> on both sides, '
                 '<code>build-pov-ray</code> to <code>povray.org</code> and a search engine on both, '
                 'and one baseline trial of <code>torch-pipeline-parallelism</code> fetched '
                 '<code>api.github.com/repos/huggingface/picotron</code> and its README — the same '
                 'reference implementation that put this side\'s trial under quarantine. Running the '
                 'same audit over the baseline\'s 439 trajectories found <b>zero trials reaching the '
                 'dataset\'s task pages, solution files or grading tests</b>. Nothing on this side '
                 'fetched an answer either: two calls were download addresses, and the rest are '
                 'documentation a tool or vendor wrote about its own product. The only search made to '
                 'settle a meaning was <code>gcode-to-text</code>, and it looked and still got it '
                 'wrong.</div></li>'),
    "rescored_tag": "rescored",
    "rescored_title": "%s — rescored to a reward of zero under the leaderboard's rule",
    "arc_title": "this trial's transcript, events and result in full (%s)",
}
