package workflow

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Resolve 把字符串里的 {{key}} 用 vars 替换（支持 dotted key：inputs.x / previous.output
// / outputs.x / 节点ID.output）。未知变量替换为空串。
func Resolve(s string, vars map[string]string) string {
	return varRe.ReplaceAllStringFunc(s, func(m string) string {
		key := strings.TrimSpace(m[2 : len(m)-2])
		if v, ok := vars[key]; ok {
			return v
		}
		return ""
	})
}

var varRe = regexp.MustCompile(`\{\{\s*([^{}]+?)\s*\}\}`)

// EvalCondition 计算条件表达式（安全子集）：
//
//	{{outputs.a}} != ""  |  {{previous.output}} contains "success"
//	{{outputs.n}} >= 100
//	expr && expr  |  expr || expr
//
// 操作符：== != > >= < <= contains matches（正则）。变量替换值一律加引号，
// 避免含空格的取值把表达式拆碎。
func EvalCondition(expr string, vars map[string]string) (bool, error) {
	s := resolveQuoted(expr, vars)
	p := &exprParser{toks: tokenize(s)}
	ok, err := p.parseOr()
	if err != nil {
		return false, err
	}
	if p.pos != len(p.toks) {
		return false, fmt.Errorf("条件末尾有未解析内容: %q", s)
	}
	return ok, nil
}

// resolveQuoted 同 Resolve，但把替换值用双引号包裹（内部引号转义）。
func resolveQuoted(s string, vars map[string]string) string {
	return varRe.ReplaceAllStringFunc(s, func(m string) string {
		key := strings.TrimSpace(m[2 : len(m)-2])
		if v, ok := vars[key]; ok {
			return `"` + strings.ReplaceAll(v, `"`, `\"`) + `"`
		}
		return `""`
	})
}

type exprTok struct {
	kind string // ident | string | num | op | and | or
	val  string
}

func tokenize(s string) []exprTok {
	var toks []exprTok
	i := 0
	for i < len(s) {
		c := s[i]
		switch {
		case c == ' ' || c == '\t':
			i++
		case c == '"' || c == '\'':
			j := i + 1
			for j < len(s) && s[j] != c {
				j++
			}
			if j >= len(s) {
				j = len(s)
			}
			toks = append(toks, exprTok{kind: "string", val: s[i+1 : j]})
			i = j + 1
		case c == '&' && i+1 < len(s) && s[i+1] == '&':
			toks = append(toks, exprTok{kind: "and", val: "&&"})
			i += 2
		case c == '|' && i+1 < len(s) && s[i+1] == '|':
			toks = append(toks, exprTok{kind: "or", val: "||"})
			i += 2
		case c == '=' || c == '!' || c == '>' || c == '<':
			// two-char or one-char operator
			j := i
			if j+1 < len(s) && s[j+1] == '=' {
				toks = append(toks, exprTok{kind: "op", val: s[j : j+2]})
				i += 2
			} else {
				toks = append(toks, exprTok{kind: "op", val: s[j : j+1]})
				i++
			}
		case isWordChar(c):
			j := i
			for j < len(s) && isWordChar(s[j]) {
				j++
			}
			tok := s[i:j]
			kind := "ident"
			if tok == "contains" || tok == "matches" {
				kind = "op"
			} else if _, err := strconv.ParseFloat(tok, 64); err == nil {
				kind = "num"
			}
			toks = append(toks, exprTok{kind: kind, val: tok})
			i = j
		default:
			i++
		}
	}
	return toks
}

func isWordChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '.'
}

type exprParser struct {
	toks []exprTok
	pos  int
}

func (p *exprParser) peek() *exprTok {
	if p.pos < len(p.toks) {
		return &p.toks[p.pos]
	}
	return nil
}

func (p *exprParser) next() *exprTok {
	t := p.peek()
	if t != nil {
		p.pos++
	}
	return t
}

func (p *exprParser) parseOr() (bool, error) {
	left, err := p.parseAnd()
	if err != nil {
		return false, err
	}
	for {
		if t := p.peek(); t != nil && t.kind == "or" {
			p.next()
			right, err := p.parseAnd()
			if err != nil {
				return false, err
			}
			left = left || right
		} else {
			return left, nil
		}
	}
}

func (p *exprParser) parseAnd() (bool, error) {
	left, err := p.parseAtom()
	if err != nil {
		return false, err
	}
	for {
		if t := p.peek(); t != nil && t.kind == "and" {
			p.next()
			right, err := p.parseAtom()
			if err != nil {
				return false, err
			}
			left = left && right
		} else {
			return left, nil
		}
	}
}

func (p *exprParser) parseAtom() (bool, error) {
	t := p.next()
	if t == nil {
		return false, fmt.Errorf("表达式意外结束")
	}
	if t.kind == "op" {
		// 一元比较：<= 0 / != "" 之类（左操作数缺省视为 "")
		op := t.val
		rhs := p.next()
		if rhs == nil {
			return false, fmt.Errorf("操作符 %s 后缺少操作数", op)
		}
		return compare("", op, rhs.val)
	}
	if t.kind == "ident" && t.val == "true" {
		return true, nil
	}
	if t.kind == "ident" && t.val == "false" {
		return false, nil
	}
	// 二元比较：left OP right
	left := t.val
	op := p.next()
	if op == nil || op.kind != "op" {
		// 裸值当作 truthy
		return left != "" && left != "false", nil
	}
	rhs := p.next()
	if rhs == nil {
		return false, fmt.Errorf("操作符 %s 后缺少右操作数", op.val)
	}
	return compare(left, op.val, rhs.val)
}

func compare(left, op, right string) (bool, error) {
	// 数值比较：若两侧都能解析为数字则按数字比较
	if l, le := strconv.ParseFloat(strings.TrimSpace(left), 64); le == nil {
		if r, re := strconv.ParseFloat(strings.TrimSpace(right), 64); re == nil {
			switch op {
			case "==":
				return l == r, nil
			case "!=":
				return l != r, nil
			case ">":
				return l > r, nil
			case ">=":
				return l >= r, nil
			case "<":
				return l < r, nil
			case "<=":
				return l <= r, nil
			}
		}
	}
	switch op {
	case "==":
		return left == right, nil
	case "!=":
		return left != right, nil
	case ">":
		return left > right, nil
	case "<":
		return left < right, nil
	case "contains":
		return strings.Contains(left, right), nil
	case "matches":
		re, err := regexp.Compile(right)
		if err != nil {
			return false, fmt.Errorf("matches 正则无效: %v", err)
		}
		return re.MatchString(left), nil
	default:
		return false, fmt.Errorf("不支持的操作符: %s", op)
	}
}
