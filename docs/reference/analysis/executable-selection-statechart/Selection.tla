----------------------------- MODULE Selection -----------------------------
EXTENDS Naturals, TLC

VARIABLES scenario, s
vars == <<scenario, s>>

Base == [external |-> FALSE, configuredDev |-> FALSE, source |-> FALSE,
         sourceBuild |-> FALSE, devApp |-> FALSE, pin |-> "v48",
         available |-> "v51", invoked |-> "dev", cached |-> FALSE,
         published |-> FALSE, index |-> 80, needsSignin |-> TRUE,
         chosen |-> "", live |-> "", mode |-> "headless",
         frontend |-> "Desktop", trusted |-> TRUE, global |-> "v51"]
Cases == [i \in 1..22 |-> CASE
 i = 1 -> Base
 [] i = 2 -> [Base EXCEPT !.available = "v48", !.global = "v48"]
 [] i = 3 -> [Base EXCEPT !.pin = "v51", !.available = "v48", !.global = "v48"]
 [] i = 4 -> [Base EXCEPT !.pin = "v51", !.cached = TRUE]
 [] i = 5 -> [Base EXCEPT !.pin = "v51", !.cached = TRUE, !.index = 78]
 [] i = 6 -> [Base EXCEPT !.external = TRUE, !.pin = "", !.index = 78, !.needsSignin = FALSE]
 [] i = 7 -> [Base EXCEPT !.external = TRUE, !.pin = "", !.chosen = "dev", !.trusted = FALSE]
 [] i = 8 -> [Base EXCEPT !.external = TRUE, !.pin = "", !.live = "dev"]
 [] i = 9 -> [Base EXCEPT !.external = TRUE, !.pin = "", !.live = "v51", !.frontend = "CLI"]
 [] i = 10 -> [Base EXCEPT !.external = TRUE, !.pin = "", !.global = ""]
 [] i = 11 -> [Base EXCEPT !.configuredDev = TRUE, !.trusted = FALSE]
 [] i = 12 -> [Base EXCEPT !.source = TRUE, !.sourceBuild = TRUE, !.trusted = FALSE]
 [] i = 13 -> [Base EXCEPT !.source = TRUE, !.devApp = TRUE, !.pin = "", !.trusted = FALSE]
 [] i = 14 -> [Base EXCEPT !.source = TRUE, !.pin = "v51", !.cached = TRUE, !.index = 78, !.needsSignin = FALSE]
 [] i = 15 -> [Base EXCEPT !.pin = "", !.invoked = "v51", !.index = 78, !.needsSignin = FALSE]
 [] i = 16 -> [Base EXCEPT !.external = TRUE, !.pin = "", !.live = "dev", !.frontend = "CLI"]
 [] i = 17 -> [Base EXCEPT !.pin = "v51", !.cached = TRUE, !.frontend = "CLI", !.index = 78, !.needsSignin = FALSE]
 [] i = 18 -> [Base EXCEPT !.index = 78]
 [] i = 19 -> [Base EXCEPT !.pin = "v51", !.cached = TRUE, !.live = "dev", !.mode = "attached", !.index = 78, !.needsSignin = FALSE]
 [] i = 20 -> [Base EXCEPT !.external = TRUE, !.pin = "", !.chosen = "v51", !.live = "dev"]
 [] i = 21 -> [Base EXCEPT !.pin = "v51", !.published = TRUE, !.index = 78, !.needsSignin = FALSE]
 [] i = 22 -> [Base EXCEPT !.source = TRUE, !.devApp = TRUE, !.trusted = FALSE]]
c == Cases[scenario]
Rank(e) == CASE e = "v48" -> 48 [] e = "v51" -> 51 [] e = "dev" -> 100 [] OTHER -> 0
Support(e) == CASE e = "dev" -> 80 [] e = "v51" -> 78 [] OTHER -> 0
Signin(e) == e = "dev"
Authority == IF c.external THEN "external"
             ELSE IF c.configuredDev \/ (c.source /\ (c.sourceBuild \/ c.devApp)) THEN "development"
             ELSE IF s.pin # "" THEN "managed" ELSE "global"
AppDevSelected == Authority = "development" /\ c.source /\ ~c.sourceBuild /\ ~c.configuredDev /\ c.devApp
RepoChosen == Authority = "managed" \/ (Authority = "development" /\ ~AppDevSelected)
NeedsTrust == RepoChosen \/ (AppDevSelected /\ s.pin # "")
Target == CASE Authority = "development" -> "dev"
          [] Authority = "managed" -> s.pin
          [] Authority = "external" -> IF s.frontend = "CLI" THEN c.invoked
                                       ELSE IF c.chosen # "" THEN c.chosen ELSE c.global
          [] OTHER -> IF s.frontend = "CLI" THEN c.invoked ELSE c.global
CanOffer == Authority = "managed" /\ ~c.source /\ Rank(s.pin) < Rank(c.available)
SilentDifference == s.frontend = "Desktop" /\ Authority = "external" /\ c.chosen = "" /\ c.live = "" /\ s.exe # c.invoked

Init == /\ scenario \in 1..22
        /\ s = [phase |-> "inspect", trusted |-> c.trusted, pin |-> c.pin,
                 frontend |-> c.frontend, switched |-> FALSE,
                 installed |-> c.cached, offer |-> FALSE, exe |-> "", live |-> c.live,
                 starter |-> IF c.frontend = "Desktop" THEN "CLI" ELSE "Desktop",
                 repoRan |-> FALSE, remedy |-> FALSE, indexNotice |-> FALSE]
Inspect == /\ s.phase = "inspect"
           /\ s' = [s EXCEPT !.phase = IF NeedsTrust /\ ~s.trusted THEN "trust" ELSE "select"]
           /\ UNCHANGED scenario
GrantTrust == /\ s.phase = "trust"
              /\ s' = [s EXCEPT !.phase = "select", !.trusted = TRUE]
              /\ UNCHANGED scenario
Resolve == /\ s.phase = "select"
           /\ s' = [s EXCEPT !.phase = IF Authority = "managed" /\ ~s.installed THEN "install"
                                       ELSE IF Authority = "external" /\ s.frontend = "Desktop" /\ c.chosen = "" /\ s.live # "" THEN "attach"
                                       ELSE IF Target = "" THEN "choose" ELSE "prepared",
                              !.exe = Target]
           /\ UNCHANGED scenario
Install == /\ s.phase = "install"
           /\ s' = [s EXCEPT !.phase = IF c.published \/ s.pin # c.pin THEN "prepared" ELSE "missing",
                              !.installed = c.published \/ s.pin # c.pin,
                              !.offer = ~(c.published \/ s.pin # c.pin) /\ CanOffer,
                              !.remedy = CanOffer]
           /\ UNCHANGED scenario
AdvancePin == /\ s.phase = "missing" /\ s.offer
              /\ s' = [s EXCEPT !.phase = "prepared", !.pin = c.available, !.exe = c.available,
                                 !.installed = TRUE, !.offer = FALSE, !.repoRan = TRUE]
              /\ UNCHANGED scenario
ChooseExecutable == /\ s.phase = "choose"
                    /\ s' = [s EXCEPT !.phase = "prepared", !.exe = "dev"]
                    /\ UNCHANGED scenario
ProbeAndEnsure == /\ s.phase = "prepared"
                  /\ s' = [s EXCEPT !.phase = IF s.live = "" THEN "spawn"
                                              ELSE IF Authority = "external" /\ s.frontend = "Desktop" THEN "attach"
                                              ELSE IF s.live = s.exe THEN "attach"
                                              ELSE IF c.mode = "attached" THEN "stopAttached" ELSE "replace",
                                     !.repoRan = @ \/ RepoChosen]
                  /\ UNCHANGED scenario
ReplaceHeadless == /\ s.phase = "replace"
                   /\ s' = [s EXCEPT !.phase = "spawn", !.live = ""]
                   /\ UNCHANGED scenario
StopAttached == /\ s.phase = "stopAttached"
                /\ s' = [s EXCEPT !.phase = "spawn", !.live = ""]
                /\ UNCHANGED scenario
Spawn == /\ s.phase = "spawn"
         /\ s' = [s EXCEPT !.phase = "initializing", !.live = s.exe, !.starter = s.frontend]
         /\ UNCHANGED scenario
Attach == /\ s.phase = "attach"
          /\ s' = [s EXCEPT !.phase = "initializing", !.exe = s.live]
          /\ UNCHANGED scenario
FinishInitialization == /\ s.phase = "initializing"
                        /\ s' = [s EXCEPT !.phase = IF Support(s.exe) < c.index THEN "preparingForever"
                                                    ELSE IF c.needsSignin /\ ~Signin(s.exe) THEN "providerUnavailable" ELSE "ready"]
                        /\ UNCHANGED scenario
OtherFrontendOpen == /\ s.phase = "ready" /\ ~s.switched
                     /\ s' = [s EXCEPT !.phase = "select", !.switched = TRUE,
                                        !.frontend = IF @ = "CLI" THEN "Desktop" ELSE "CLI"]
                     /\ UNCHANGED scenario
Done == /\ s.phase \in {"missing", "preparingForever", "providerUnavailable", "ready"}
        /\ UNCHANGED vars
Next == Inspect \/ GrantTrust \/ Resolve \/ Install \/ AdvancePin \/ ChooseExecutable \/
        ProbeAndEnsure \/ ReplaceHeadless \/ StopAttached \/ Spawn \/ Attach \/ FinishInitialization \/ OtherFrontendOpen \/ Done

ActionableError == s.phase = "missing" => s.remedy
NoEndlessPreparing == s.phase # "preparingForever"
SameExecutableOrExplained == s.phase \notin {"ready", "providerUnavailable", "preparingForever"} \/ ~SilentDifference
IndexCompatibilityVisible == s.phase # "preparingForever" \/ s.indexNotice
OfferPreservesIndex == s.offer => Support(c.available) >= c.index
OfferPreservesSignin == s.offer => (~c.needsSignin \/ Signin(c.available))
TrustBeforeRepoCode == ~s.repoRan \/ s.trusted
AppOwnedNeedsNoTrust == ~(s.phase = "trust" /\ c.source /\ c.devApp /\ ~c.sourceBuild /\ ~c.configuredDev)
=============================================================================
