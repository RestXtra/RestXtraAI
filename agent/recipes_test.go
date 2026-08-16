package agent

import (
	"encoding/json"
	"testing"
)

func TestRecipesLoadAndRender(t *testing.T) {
	recipes, err := LoadRecipes()
	if err != nil {
		t.Fatalf("LoadRecipes: %v", err)
	}
	if len(recipes) == 0 {
		t.Fatal("expected at least one tool recipe")
	}
	byName := map[string]ToolRecipe{}
	for _, r := range recipes {
		byName[r.Name] = r
	}
	nm, ok := byName["nmap"]
	if !ok {
		t.Fatalf("nmap recipe missing; got %d recipes", len(recipes))
	}
	// nmap: target(position 1) + ports(flag -p) + timing(template -T{value})
	cmd, err := renderRecipeCommand(nm, json.RawMessage(`{"target":"10.0.0.1","ports":"80,443","timing":"4"}`))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	want := "nmap 10.0.0.1 -p 80,443 -T4 -sT -sV -sC"
	if cmd != want {
		t.Errorf("render mismatch:\n got  %s\n want %s", cmd, want)
	}
	// boolean flag renders as bare flag (subfinder -silent)
	sf, ok := byName["subfinder"]
	if !ok {
		t.Fatalf("subfinder recipe missing")
	}
	cmd2, err := renderRecipeCommand(sf, json.RawMessage(`{"domain":"example.com","silent":true}`))
	if err != nil {
		t.Fatalf("render subfinder: %v", err)
	}
	if cmd2 != "subfinder -d example.com -silent" {
		t.Errorf("subfinder render got %q", cmd2)
	}
	// RecipeSeeds: nmap bound to worker
	seeds := RecipeSeeds()
	found := false
	for _, s := range seeds {
		if s.Key == "nmap" {
			found = true
			if len(s.Agents) == 0 || s.Agents[0] != "worker" {
				t.Errorf("nmap seed agents = %v, want [worker]", s.Agents)
			}
		}
	}
	if !found {
		t.Error("nmap not in RecipeSeeds")
	}
}
