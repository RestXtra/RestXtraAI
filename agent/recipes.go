package agent

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Autumn-27/norma/permission"
	actool "github.com/Autumn-27/norma/tool"
	"gopkg.in/yaml.v3"
)

// 工具配方：以数据驱动（YAML）把 CLI 渗透工具接入 agent。
// toolrecipes/*.yaml 定义命令、参数格式(positional/flag/combined/template)与
// short_description（压 token）。启动时解析 → 构建 CoreTool → 播种进 tools 表，
// 运行时渲染命令行并复用 Bash 底层执行（继承代理/超时/拦截）。

//go:embed toolrecipes/*.yaml
var recipesFS embed.FS

// ToolRecipe 是一个 YAML 工具配方。
type ToolRecipe struct {
	Name             string        `yaml:"name"`
	Command          string        `yaml:"command"`
	Description      string        `yaml:"description"`
	ShortDescription string        `yaml:"short_description"`
	Enabled          *bool         `yaml:"enabled"` // 未声明默认启用；显式 false 关闭
	Parameters       []RecipeParam `yaml:"parameters"`
	AdditionalArgs   []string      `yaml:"additional_args"`
}

// RecipeParam 描述一个命令行参数。
type RecipeParam struct {
	Name     string `yaml:"name"`
	Type     string `yaml:"type"` // string|boolean|integer
	Required bool   `yaml:"required"`
	Position int    `yaml:"position"`
	Flag     string `yaml:"flag"`
	Template string `yaml:"template"`
	Format   string `yaml:"format"` // positional|flag|combined|template
}

// LoadRecipes 解析内嵌的 toolrecipes/*.yaml。
func LoadRecipes() ([]ToolRecipe, error) {
	entries, err := recipesFS.ReadDir("toolrecipes")
	if err != nil {
		return nil, err
	}
	var out []ToolRecipe
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		b, err := recipesFS.ReadFile("toolrecipes/" + e.Name())
		if err != nil {
			return nil, err
		}
		var r ToolRecipe
		if err := yaml.Unmarshal(b, &r); err != nil {
			return nil, fmt.Errorf("解析 %s: %w", e.Name(), err)
		}
		if r.Name == "" || r.Command == "" {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

// RecipeTools 构建全部启用的配方工具。
func RecipeTools() []actool.CoreTool {
	recipes, err := LoadRecipes()
	if err != nil {
		return nil
	}
	var out []actool.CoreTool
	for _, r := range recipes {
		if !recipeEnabled(r) {
			continue
		}
		out = append(out, buildRecipeTool(r))
	}
	return out
}

// recipeEnabled 判断配方是否启用：YAML 未声明 enabled 时默认启用，显式 false 关闭。
func recipeEnabled(r ToolRecipe) bool {
	return r.Enabled == nil || *r.Enabled
}

// RecipeSeeds 生成 tools 表播种快照（默认绑 worker）。
func RecipeSeeds() []ToolSeed {
	recipes, err := LoadRecipes()
	if err != nil {
		return nil
	}
	var out []ToolSeed
	for _, r := range recipes {
		if !recipeEnabled(r) {
			continue
		}
		desc := r.ShortDescription
		if desc == "" {
			desc = r.Description
		}
		out = append(out, ToolSeed{Key: r.Name, Desc: desc, Schema: recipeSchema(r), Agents: []string{"worker"}})
	}
	return out
}

func recipeSchema(r ToolRecipe) map[string]any {
	props := map[string]any{}
	var req []any
	for _, p := range r.Parameters {
		t := "string"
		switch p.Type {
		case "boolean":
			t = "boolean"
		case "integer":
			t = "integer"
		}
		props[p.Name] = map[string]any{"type": t, "description": recipeParamDesc(p)}
		if p.Required {
			req = append(req, p.Name)
		}
	}
	out := map[string]any{"type": "object", "properties": props}
	if len(req) > 0 {
		out["required"] = req
	}
	return out
}

func recipeParamDesc(p RecipeParam) string {
	d := p.Name
	if p.Required {
		d += "（必填）"
	}
	return d
}

func buildRecipeTool(r ToolRecipe) actool.CoreTool {
	desc := r.ShortDescription
	if desc == "" {
		desc = r.Description
	}
	return actool.Build(actool.Spec{
		Name:        r.Name,
		Description: desc,
		Schema:      recipeSchema(r),
		ReadOnly:    func(json.RawMessage) bool { return true },
		Concurrent:  func(json.RawMessage) bool { return true },
		Permissions: func(ctx context.Context, _ json.RawMessage, _ permission.Context) permission.Decision {
			return permission.Allowed()
		},
		Run: func(ctx context.Context, in json.RawMessage, tc *actool.ToolContext) (actool.Result, error) {
			cmd, err := renderRecipeCommand(r, in)
			if err != nil {
				return actool.Errorf(err.Error()), nil
			}
			bashIn, _ := json.Marshal(map[string]any{"command": cmd})
			return HostBash().Call(ctx, bashIn, tc)
		},
	})
}

// renderRecipeCommand 按参数格式把 JSON 参数渲染成命令行。
func renderRecipeCommand(r ToolRecipe, in json.RawMessage) (string, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(in, &m); err != nil {
		return "", fmt.Errorf("参数解析失败: %v", err)
	}
	pos := map[int]string{}
	var flags []string
	for _, p := range r.Parameters {
		val := recipeParamValue(m, p)
		if val == "" {
			continue
		}
		switch p.Format {
		case "positional":
			if p.Position > 0 {
				pos[p.Position] = val
			} else {
				flags = append(flags, val)
			}
		case "flag":
			if p.Type == "boolean" {
				flags = append(flags, p.Flag)
			} else {
				flags = append(flags, p.Flag, val)
			}
		case "combined":
			flags = append(flags, p.Flag+"="+val)
		case "template":
			flags = append(flags, strings.ReplaceAll(p.Template, "{value}", val))
		default:
			flags = append(flags, val)
		}
	}
	parts := []string{r.Command}
	for i := 1; i <= len(pos); i++ {
		if v := pos[i]; v != "" {
			parts = append(parts, v)
		}
	}
	parts = append(parts, flags...)
	parts = append(parts, r.AdditionalArgs...)
	return strings.Join(parts, " "), nil
}

func recipeParamValue(m map[string]json.RawMessage, p RecipeParam) string {
	raw, ok := m[p.Name]
	if !ok {
		return ""
	}
	if p.Type == "boolean" {
		var b bool
		if json.Unmarshal(raw, &b) == nil && b {
			return "true"
		}
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	return ""
}
