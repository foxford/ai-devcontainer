package runtime

import (
	"regexp"
	"strconv"
	"strings"
)

var exactVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)
var partialVersion = regexp.MustCompile(`^(?:v)?([0-9]+|[xX*])(?:\.([0-9]+|[xX*]))?(?:\.([0-9]+|[xX*]))?(?:-([0-9A-Za-z.-]+))?(?:\+[0-9A-Za-z.-]+)?$`)
var rangeOperatorSpace = regexp.MustCompile(`(\^|~|>=|<=|>|<|=)\s+`)

type semVersion struct {
	numbers [3]int
	pre     string
	parts   int
}

func parseSemVersion(text string) (semVersion, bool) {
	m := partialVersion.FindStringSubmatch(text)
	v := semVersion{}
	if m == nil {
		return v, false
	}
	for index := 0; index < 3; index++ {
		if m[index+1] == "" || strings.ContainsAny(m[index+1], "xX*") {
			break
		}
		n, err := strconv.Atoi(m[index+1])
		if err != nil {
			return v, false
		}
		v.numbers[index] = n
		v.parts++
	}
	v.pre = m[4]
	return v, true
}
func compareVersion(a, b semVersion) int {
	for index := range a.numbers {
		if a.numbers[index] < b.numbers[index] {
			return -1
		}
		if a.numbers[index] > b.numbers[index] {
			return 1
		}
	}
	if a.pre == b.pre {
		return 0
	}
	if a.pre == "" {
		return 1
	}
	if b.pre == "" {
		return -1
	}
	aa, bb := strings.Split(a.pre, "."), strings.Split(b.pre, ".")
	for index := 0; index < len(aa) && index < len(bb); index++ {
		if aa[index] == bb[index] {
			continue
		}
		an, ae := strconv.Atoi(aa[index])
		bn, be := strconv.Atoi(bb[index])
		if ae == nil && be == nil {
			if an < bn {
				return -1
			}
			return 1
		}
		if ae == nil {
			return -1
		}
		if be == nil {
			return 1
		}
		if aa[index] < bb[index] {
			return -1
		}
		return 1
	}
	if len(aa) < len(bb) {
		return -1
	}
	return 1
}
func comparatorAllows(target semVersion, text string) (bool, bool) {
	op := ""
	for _, prefix := range []string{">=", "<=", ">", "<", "=", "^", "~"} {
		if strings.HasPrefix(text, prefix) {
			op = prefix
			text = strings.TrimPrefix(text, prefix)
			break
		}
	}
	base, ok := parseSemVersion(text)
	if !ok {
		return false, false
	}
	if base.parts == 0 {
		if op == ">" || op == "<" {
			return false, true
		}
		return true, true
	}
	cmp := compareVersion(target, base)
	upper := base
	upper.pre = ""
	nextPartial := func() {
		if base.parts == 1 {
			upper.numbers = [3]int{base.numbers[0] + 1, 0, 0}
		} else {
			upper.numbers = [3]int{base.numbers[0], base.numbers[1] + 1, 0}
		}
	}
	switch op {
	case "^":
		if base.numbers[0] > 0 || base.parts == 1 {
			upper.numbers = [3]int{base.numbers[0] + 1, 0, 0}
		} else if base.numbers[1] > 0 || base.parts == 2 {
			upper.numbers = [3]int{0, base.numbers[1] + 1, 0}
		} else {
			upper.numbers = [3]int{0, 0, base.numbers[2] + 1}
		}
		return cmp >= 0 && compareVersion(target, upper) < 0, true
	case "~":
		nextPartial()
		return cmp >= 0 && compareVersion(target, upper) < 0, true
	case ">=":
		return cmp >= 0, true
	case ">":
		if base.parts < 3 {
			nextPartial()
			return compareVersion(target, upper) >= 0, true
		}
		return cmp > 0, true
	case "<=":
		if base.parts < 3 {
			nextPartial()
			return compareVersion(target, upper) < 0, true
		}
		return cmp <= 0, true
	case "<":
		return cmp < 0, true
	default:
		if base.parts < 3 {
			nextPartial()
			return cmp >= 0 && compareVersion(target, upper) < 0, true
		}
		return cmp == 0, true
	}
}

// Unsupported registry aliases/protocols stay unknown; doctor never treats them
// as evidence that an override can safely be removed.
func rangeAllows(version, rangeText string) (bool, bool) {
	if strings.TrimSpace(rangeText) == "" {
		return false, false
	}
	target, ok := parseSemVersion(version)
	if !ok || target.parts != 3 {
		return false, false
	}
	unknown := false
	for _, branch := range strings.Split(rangeText, "||") {
		branch = strings.TrimSpace(branch)
		if lo, hi, ok := strings.Cut(branch, " - "); ok {
			upper, valid := parseSemVersion(strings.TrimSpace(hi))
			if !valid {
				return false, false
			}
			op := "<="
			if upper.parts < 3 {
				if upper.parts == 1 {
					hi = strconv.Itoa(upper.numbers[0]+1) + ".0.0"
				} else {
					hi = strconv.Itoa(upper.numbers[0]) + "." + strconv.Itoa(upper.numbers[1]+1) + ".0"
				}
				op = "<"
			}
			branch = ">=" + strings.TrimSpace(lo) + " " + op + strings.TrimSpace(hi)
		}
		branch = rangeOperatorSpace.ReplaceAllString(branch, "$1")
		matches, known, preAllowed := true, true, target.pre == ""
		for _, token := range strings.Fields(branch) {
			allowed, valid := comparatorAllows(target, token)
			if !valid {
				known = false
			}
			if !allowed {
				matches = false
			}
			base, _ := parseSemVersion(strings.TrimLeft(token, "<>~=^"))
			if base.pre != "" && base.numbers == target.numbers {
				preAllowed = true
			}
		}
		if matches && known && preAllowed {
			return true, true
		}
		if !known {
			unknown = true
		}
	}
	return false, !unknown
}
func touchesMajor(rangeText string, major int) bool {
	min, max, found := 0, 0, false
	for _, token := range strings.Fields(strings.ReplaceAll(rangeText, "||", " ")) {
		base, ok := parseSemVersion(strings.TrimLeft(token, "<>~=^"))
		if !ok || base.parts == 0 {
			continue
		}
		m := base.numbers[0]
		if !found {
			min, max, found = m, m, true
		}
		if m < min {
			min = m
		}
		if m > max {
			max = m
		}
	}
	return !found || major >= min && major <= max
}
