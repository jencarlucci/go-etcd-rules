package rules

import (
	"strings"
)

type ruleManager struct {
	rulesBySlashCount map[int]map[DynamicRule]int
	prefixes          map[string]string
	currentIndex      int
}

func newRuleManager() ruleManager {
	rm := ruleManager{
		rulesBySlashCount: map[int]map[DynamicRule]int{},
		prefixes:          map[string]string{},
		currentIndex:      0,
	}
	return rm
}

func (rm *ruleManager) getStaticRules(key string, value *string) map[staticRule]int {
	slashCount := strings.Count(key, "/")
	out := make(map[staticRule]int)
	rules, ok := rm.rulesBySlashCount[slashCount]
	if ok {
		for rule, index := range rules {
			sRule, _, inScope := rule.makeStaticRule(key, value)
			if inScope && sRule.satisfiable(key, value) {
				out[sRule] = index
			}
		}
	}
	return out
}

func (rm *ruleManager) addRule(rule DynamicRule) int {
	for _, pattern := range rule.getPatterns() {
		slashCount := strings.Count(pattern, "/")
		rules, ok := rm.rulesBySlashCount[slashCount]
		if !ok {
			rules = map[DynamicRule]int{}
			rm.rulesBySlashCount[slashCount] = rules
		}
		rules[rule] = rm.currentIndex
	}
	for _, prefix := range rule.getPrefixes() {
		rm.prefixes[prefix] = ""
	}
	lastIndex := rm.currentIndex
	rm.currentIndex = rm.currentIndex + 1
	return lastIndex
}