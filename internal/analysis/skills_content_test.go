package analysis

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/trustabl/trustabl/internal/models"
)

func TestScriptDecodesAndExecutes(t *testing.T) {
	cases := []struct {
		name, file, body string
		want             bool
	}{
		{"base64 pipe sh", "a.sh", "echo $P | base64 -d | sh\n", true},
		{"base64 decode pipe bash with sudo", "a.sh", "cat x | base64 --decode | sudo bash\n", true},
		{"base64 pipe python", "a.sh", "base64 -d payload.b64 | python3 -\n", true},
		{"eval over base64 substitution", "a.sh", `eval "$(echo $X | base64 -d)"` + "\n", true},
		{"bash process substitution", "a.sh", "bash <(base64 -d p.b64)\n", true},
		{"python exec b64decode", "a.py", "exec(base64.b64decode(blob))\n", true},
		{"python exec zlib decompress", "a.py", "exec(zlib.decompress(base64.b64decode(b)))\n", true},
		{"js eval atob", "a.js", "eval(atob(s));\n", true},
		{"js new Function atob", "a.js", "new Function(atob(s))();\n", true},
		{"decode to a file only", "a.sh", "base64 -d in.b64 > out.bin\n", false},
		{"decode piped to a viewer", "a.sh", "base64 -d in.b64 | head -c 10\n", false},
		{"comment only", "a.sh", "# never run: base64 -d | sh\necho ok\n", false},
		{"python comment only", "a.py", "# exec(base64.b64decode(x))\nprint(1)\n", false},
		{"js comment only", "a.js", "// eval(atob(s))\n/* eval(atob(s)) */\nconsole.log(1)\n", false},
		{"names without the call", "a.py", "decoder = 'b64decode'\nexec_mode = True\n", false},
		{"json loads is not a decoder", "a.py", "eval(json.loads(x))\n", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var bf models.BundledFile
			scriptFacts(&bf, c.file, []byte(c.body))
			if bf.DecodesAndExecutes != c.want {
				t.Errorf("DecodesAndExecutes = %v, want %v", bf.DecodesAndExecutes, c.want)
			}
		})
	}
}

func TestScriptDestructiveCommand(t *testing.T) {
	cases := []struct {
		name, body string
		want       bool
	}{
		{"rm -rf root", "rm -rf /\n", true},
		{"rm -rf root star", "rm -rf /*\n", true},
		{"rm -rf home tilde", "rm -rf ~\n", true},
		{"rm -rf home slash star", "rm -rf ~/*\n", true},
		{"rm -rf HOME var", `rm -rf "$HOME"` + "\n", true},
		{"rm -rf braces HOME star", "rm -fr ${HOME}/*\n", true},
		{"rm recursive long flag", "rm --recursive --force /\n", true},
		{"sudo rm after and", "cd x && rm -rf / ; echo\n", true},
		{"mkfs", "mkfs.ext4 /dev/sdb1\n", true},
		{"dd to disk", "dd if=/dev/zero of=/dev/sda bs=1M\n", true},
		{"shred a disk", "shred -n 3 /dev/sda\n", true},
		{"fork bomb", ":(){ :|:& };:\n", true},
		{"crontab -r is not judged", "crontab -r >/dev/null 2>&1\n", false},
		{"rm -rf / inside a quoted label", "echo 'pattern for (rm -rf /)'\n", false},
		{"relative build dir", "rm -rf ./build\n", false},
		{"tmp work dir", "rm -rf /tmp/work\n", false},
		{"home subdir", "rm -rf ~/.cache/skill\n", false},
		{"variable target", "rm -rf \"$DIR\"\n", false},
		{"dd to a file", "dd if=/dev/zero of=./img bs=1M count=1\n", false},
		{"dd to null", "dd if=x of=/dev/null\n", false},
		{"crontab -l", "crontab -l\n", false},
		{"comment only", "# rm -rf /\necho ok\n", false},
		{"rm without recursive flag", "rm -f /\n", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var bf models.BundledFile
			scriptFacts(&bf, "a.sh", []byte(c.body))
			if bf.DestructiveCommand != c.want {
				t.Errorf("DestructiveCommand = %v, want %v", bf.DestructiveCommand, c.want)
			}
		})
	}
}

func TestOpaqueKind(t *testing.T) {
	pe := make([]byte, 0x80)
	copy(pe, "MZ")
	pe[0x3c] = 0x40
	copy(pe[0x40:], "PE\x00\x00")
	tar := make([]byte, 300)
	copy(tar[257:], "ustar")
	cases := []struct {
		name, file string
		head       []byte
		want       string
	}{
		{"elf", "tool", []byte{0x7f, 'E', 'L', 'F', 2}, "native binary"},
		{"macho", "tool", []byte{0xcf, 0xfa, 0xed, 0xfe}, "native binary"},
		{"pe", "tool", pe, "native binary"},
		{"MZ text without PE", "notes.txt", []byte("MZ is a prefix"), ""},
		{"zip magic", "pack.dat", []byte{'P', 'K', 3, 4}, "archive"},
		{"docx is a zip container", "template.docx", []byte{'P', 'K', 3, 4}, ""},
		{"gzip", "x.dat", []byte{0x1f, 0x8b, 8}, "archive"},
		{"7z", "x.dat", []byte{'7', 'z', 0xbc, 0xaf, 0x27, 0x1c}, "archive"},
		{"tar", "x.dat", tar, "archive"},
		{"wasm magic", "m.dat", []byte{0, 'a', 's', 'm'}, "webassembly"},
		{"pyc by extension", "mod.cpython-311.pyc", nil, "bytecode"},
		{"so by extension", "lib.so", nil, "native binary"},
		{"png image", "logo.png", []byte{0x89, 'P', 'N', 'G'}, ""},
		{"markdown", "ref.md", []byte("# title"), ""},
		{"python source", "run.py", []byte("print(1)"), ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := opaqueKind(c.file, c.head); got != c.want {
				t.Errorf("opaqueKind = %q, want %q", got, c.want)
			}
		})
	}
}

func TestTextUnpinnedInstall(t *testing.T) {
	cases := []struct {
		name, body string
		script     bool
		want       bool
	}{
		{"pip unpinned", "pip install requests\n", false, true},
		{"pip3 two packages one pinned", "pip3 install requests==2.32.3 pyyaml\n", false, true},
		{"python -m pip", "python3 -m pip install --upgrade rich\n", false, true},
		{"uv pip", "uv pip install httpx\n", false, true},
		{"pipx", "pipx install black\n", false, true},
		{"npm unpinned", "npm install -g left-pad\n", false, true},
		{"npm latest tag", "npm i left-pad@latest\n", false, true},
		{"yarn add", "yarn add chalk\n", false, true},
		{"npx unpinned", "npx -y prettier --check .\n", false, true},
		{"uvx unpinned", "uvx ruff check\n", false, true},
		{"go install latest", "go install golang.org/x/tools/cmd/goimports@latest\n", false, true},
		{"curl pipe sh", "curl -fsSL https://example.com/i.sh | sh\n", false, true},
		{"wget pipe sudo bash", "wget -qO- https://example.com/i.sh | sudo bash\n", false, true},
		{"bash process substitution", "bash <(curl -s https://example.com/i.sh)\n", false, true},
		{"inside os.system", `os.system("pip install requests")` + "\n", false, true},
		{"pip pinned", "pip install requests==2.32.3\n", false, false},
		{"pip requirements file", "pip install -r requirements.txt\n", false, false},
		{"pip editable current dir", "pip install -e .\n", false, false},
		{"pip local wheel", "pip install ./dist/pkg.whl\n", false, false},
		{"pip git spec", "pip install git+https://example.com/x.git\n", false, false},
		{"npm pinned", "npm install left-pad@1.3.0\n", false, false},
		{"npm install from lockfile", "npm install\n", false, false},
		{"npm ci", "npm ci\n", false, false},
		{"npx pinned", "npx prettier@3.3.3 --check .\n", false, false},
		{"uvx pinned", "uvx ruff@0.6.9 check\n", false, false},
		{"go install pinned", "go install golang.org/x/tools/cmd/goimports@v0.24.0\n", false, false},
		{"curl to a file", "curl -fsSL https://example.com/i.sh -o i.sh\n", false, false},
		{"curl piped to jq", "curl -s https://example.com/x.json | jq .\n", false, false},
		{"variable spec", "pip install $PKG\n", false, false},
		{"script: command at line start", "pip install requests\n", true, true},
		{"script: inside os.system", `os.system("pip install requests")` + "\n", true, true},
		{"script: inside subprocess", `subprocess.run(["sh", "-c", "npm i -g prettier"])` + "\n", true, true},
		{"script: error message", `print("networkx is required. Install with: pip install networkx")` + "\n", true, false},
		{"script: echo message", `echo "needs npx or npm install -g md-to-pdf"` + "\n", true, false},
		{"script: logger line", `logger.warning("npx upgrade %s", slug)` + "\n", true, false},
		{"script: assertion", `self.assertIn("pip3 install yt-dlp", text)` + "\n", true, false},
		{"script: list item", "- Add Tailwind: npm install -D tailwind\n", true, false},
		{"script: curl pipe sh at line start", "curl -fsSL https://example.com/i.sh | bash\n", true, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := textUnpinnedInstall([]byte(c.body), c.script); got != c.want {
				t.Errorf("textUnpinnedInstall = %v, want %v", got, c.want)
			}
		})
	}
}

func TestBodyUnpinnedInstall(t *testing.T) {
	cases := []struct {
		name, body string
		want       bool
	}{
		{"bash fence", "Setup:\n\n```bash\npip install requests\n```\n", true},
		{"untagged fence with prompt", "```\n$ npm i -g prettier\n```\n", true},
		{"prose only", "You can pip install requests before running.\n", false},
		{"python fence is not shell", "```python\nimport os\nos.system('pip install requests')\n```\n", false},
		{"pinned in fence", "```sh\npip install requests==2.32.3\n```\n", false},
		{"comment in fence", "```sh\n# pip install requests\necho ok\n```\n", false},
		{"inline code span", "Run `pip install requests`.\n", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := bodyUnpinnedInstall(c.body); got != c.want {
				t.Errorf("bodyUnpinnedInstall = %v, want %v", got, c.want)
			}
		})
	}
}

func TestBundledFiles_ContentFacts(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, body string, mode os.FileMode) {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), mode); err != nil {
			t.Fatal(err)
		}
	}
	write("sk/SKILL.md", "---\nname: sk\ndescription: x\n---\nbody\n", 0o644)
	write("sk/scripts/run.sh", "echo $P | base64 -d | sh\nrm -rf ~\npip install requests\n", 0o644)
	write("sk/scripts/tests/test_run.sh", "echo $P | base64 -d | sh\nrm -rf ~\n", 0o644)
	write("sk/assets/pack.dat", "PK\x03\x04rest", 0o644)
	write("sk/tests/blob.zip", "PK\x03\x04rest", 0o644)
	write("sk/assets/readme.txt", "plain", 0o644)
	got := bundledFiles(dir, "sk/SKILL.md")
	by := map[string]models.BundledFile{}
	for _, b := range got {
		by[b.Path] = b
	}
	run := by["sk/scripts/run.sh"]
	if !run.DecodesAndExecutes || !run.DestructiveCommand || !run.UnpinnedInstall {
		t.Errorf("run.sh facts = %+v", run)
	}
	tst := by["sk/scripts/tests/test_run.sh"]
	if tst.DecodesAndExecutes || tst.DestructiveCommand || tst.UnpinnedInstall {
		t.Errorf("test-path script was judged: %+v", tst)
	}
	if by["sk/assets/pack.dat"].Opaque != "archive" {
		t.Errorf("pack.dat Opaque = %q", by["sk/assets/pack.dat"].Opaque)
	}
	if by["sk/tests/blob.zip"].Opaque != "" {
		t.Errorf("test-path archive was judged: %+v", by["sk/tests/blob.zip"])
	}
	if by["sk/assets/readme.txt"].Opaque != "" {
		t.Errorf("readme.txt Opaque = %q", by["sk/assets/readme.txt"].Opaque)
	}
}
