package analysis

import (
	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
)

// pyImportAliases maps the names a Python file binds from the modules isModule
// accepts. `from m import a` binds a → a; `from m import a as b` binds b → a;
// `import m` binds m → "*" and `import m as x` binds x → "*", "*" marking a
// module alias whose attributes are the module's names.
func pyImportAliases(pf ParsedFile, isModule func(string) bool) map[string]string {
	out := map[string]string{}
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		switch n.Type() {
		case "import_from_statement":
			if !isModule(astutil.NodeText(n.ChildByFieldName("module_name"), pf.Source)) {
				return true
			}
			for i := 0; i < int(n.ChildCount()); i++ {
				if n.FieldNameForChild(i) != "name" {
					continue
				}
				c := n.Child(i)
				switch c.Type() {
				case "dotted_name":
					name := astutil.NodeText(c, pf.Source)
					out[name] = name
				case "aliased_import":
					out[astutil.NodeText(c.ChildByFieldName("alias"), pf.Source)] =
						astutil.NodeText(c.ChildByFieldName("name"), pf.Source)
				}
			}
		case "import_statement":
			for i := 0; i < int(n.NamedChildCount()); i++ {
				c := n.NamedChild(i)
				switch c.Type() {
				case "dotted_name":
					if name := astutil.NodeText(c, pf.Source); isModule(name) {
						out[name] = "*"
					}
				case "aliased_import":
					if isModule(astutil.NodeText(c.ChildByFieldName("name"), pf.Source)) {
						out[astutil.NodeText(c.ChildByFieldName("alias"), pf.Source)] = "*"
					}
				}
			}
		}
		return true
	})
	return out
}
