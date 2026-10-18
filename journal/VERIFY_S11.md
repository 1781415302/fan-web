# VERIFY_S11

## Verdict: PASS

## list
```
TestMatchingEndToEnd
TestMatchingCorpusIntegration
ok  	fan-web/services	0.004s
```

## EndToEnd + CorpusIntegration
```
--- PASS: TestMatchingEndToEnd (0.26s)
e2e summary: auto_ok=18 wrong_accept=0 review=9 no_candidates=2 cases=23

--- PASS: TestMatchingCorpusIntegration (0.38s)
effect gate: v2_correct=29 legacy_correct=12 v2_wrong=0 legacy_wrong=1 files=29
gains (17); losses (0)
```

## Holdout
```
--- PASS: TestMatchingQualityHoldout (0.05s)
holdout summary: variants=31 auto_ok=47 wrong_accept=0 review=0 no_candidates=0 failures=0
```

## package
```
ok  	fan-web/services	2.647s
ok  	fan-web/services/matching	0.084s
```

## Notes
- 无 SKIP_ATTACHMENT_GAP
- 未改 S6 标注；未据 holdout 调分
