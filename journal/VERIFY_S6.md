# VERIFY_S6

## Verdict: PASS

## list
```
TestMatchingQuality
ok  	fan-web/services/matching	0.002s
```

## TestMatchingQuality -v (tail)
```
    quality_test.go:209:   bucket catalog: auto_ok=1 wrong_accept=0 review=0 no_candidates=0
    quality_test.go:209:   bucket chinese_blocks: auto_ok=2 wrong_accept=0 review=0 no_candidates=0
    quality_test.go:209:   bucket conflict: auto_ok=0 wrong_accept=0 review=2 no_candidates=0
    quality_test.go:209:   bucket directory: auto_ok=1 wrong_accept=0 review=0 no_candidates=0
    quality_test.go:209:   bucket long: auto_ok=1 wrong_accept=0 review=0 no_candidates=0
    quality_test.go:209:   bucket mixed: auto_ok=5 wrong_accept=0 review=2 no_candidates=0
    quality_test.go:209:   bucket negative: auto_ok=0 wrong_accept=0 review=0 no_candidates=2
    quality_test.go:209:   bucket scene: auto_ok=1 wrong_accept=0 review=0 no_candidates=0
    quality_test.go:209:   bucket season: auto_ok=1 wrong_accept=0 review=0 no_candidates=0
    quality_test.go:209:   bucket title_number: auto_ok=2 wrong_accept=0 review=0 no_candidates=0
    quality_test.go:209:   bucket unsupported: auto_ok=0 wrong_accept=0 review=3 no_candidates=0
    quality_test.go:209:   bucket year: auto_ok=2 wrong_accept=0 review=0 no_candidates=0
    quality_test.go:221: holdout frozen variants=31 (not scored in this run; final verification only)
--- PASS: TestMatchingQuality (0.03s)
    --- PASS: TestMatchingQuality/xian-gm-s4 (0.00s)
    --- PASS: TestMatchingQuality/xian-gm-s3 (0.00s)
    --- PASS: TestMatchingQuality/xian-en-s5 (0.00s)
    --- PASS: TestMatchingQuality/xian-three-seasons (0.01s)
    --- PASS: TestMatchingQuality/remake-two-years (0.00s)
    --- PASS: TestMatchingQuality/scene-s2 (0.00s)
    --- PASS: TestMatchingQuality/chinese-season (0.00s)
    --- PASS: TestMatchingQuality/absolute (0.00s)
    --- PASS: TestMatchingQuality/long-number (0.00s)
    --- PASS: TestMatchingQuality/number-title (0.00s)
    --- PASS: TestMatchingQuality/ranma-correct (0.00s)
    --- PASS: TestMatchingQuality/movie-series-mix (0.00s)
    --- PASS: TestMatchingQuality/directory-only (0.00s)
    --- PASS: TestMatchingQuality/directory-not-identity (0.00s)
    --- PASS: TestMatchingQuality/unknown (0.00s)
    --- PASS: TestMatchingQuality/season-ambiguity (0.00s)
    --- PASS: TestMatchingQuality/fragment-conflict (0.00s)
    --- PASS: TestMatchingQuality/range (0.00s)
    --- PASS: TestMatchingQuality/fraction (0.00s)
    --- PASS: TestMatchingQuality/special (0.00s)
    --- PASS: TestMatchingQuality/cross-language-chapter-duplicate (0.00s)
    --- PASS: TestMatchingQuality/ordinal-conflict (0.00s)
    --- PASS: TestMatchingQuality/year-conflict (0.00s)
    --- PASS: TestMatchingQuality/plan45_parse_extras (0.00s)
PASS
ok  	fan-web/services/matching	0.036s
```

## package
```
ok  	fan-web/services/matching	0.037s
```
