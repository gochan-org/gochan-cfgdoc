package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const (
	configHeader = `# Configuration
See [gochan.example.json](examples/configs/gochan.example.json) for an example gochan.json.

**Make sure gochan has read-write permission for ` + "`" + `DocumentRoot` + "`" + ` and ` + "`" + `LogDir` + "`" + ` and read permission for ` + "`" + `TemplateDir` + "`" + `**

Fields in the table marked as board options can be overridden on individual boards by adding them to  board.json, which gochan looks for in the board directory or in the same directory as gochan.json.

`
)

var (
	compositeStructTypes = []string{
		"SystemCriticalConfig", "SQLConfig", "SiteConfig", "BoardConfig", "PostConfig", "UploadConfig",
	}
	explicitlyNamedStructTypes = []string{
		"CaptchaConfig", "PageBanner", "BoardCooldowns",
	}
)

type columnLengths struct {
	fieldLength   int
	typeLength    int
	defaultLength int
	docLength     int
}

func (c *columnLengths) setLengths(strs ...structType) {
	c.fieldLength = 6
	c.typeLength = 5
	c.defaultLength = 0
	c.docLength = 4
	for _, str := range strs {
		for _, field := range str.fields {
			if len(field.name) > c.fieldLength {
				c.fieldLength = len(field.name)
			}
			if len(field.fType) > c.typeLength {
				c.typeLength = len(field.fType)
			}
			if len(field.defaultVal) > c.defaultLength {
				c.defaultLength = len(field.defaultVal)
			}
			if len(field.doc) > c.docLength {
				c.docLength = len(field.doc)
			}
		}
	}
	if c.defaultLength > 0 && c.defaultLength < 8 {
		c.defaultLength = 8
	}
}

func mustParse(fset *token.FileSet, filename, filePath string) *ast.File {
	ba, err := os.ReadFile(filePath)
	if err != nil {
		panic(err)
	}

	f, err := parser.ParseFile(fset, filename, string(ba), parser.ParseComments|parser.DeclarationErrors)
	if err != nil {
		panic(err)
	}
	return f
}

type structType struct {
	name   string
	doc    string
	fields []fieldType
}

func (s *structType) isBoardConfig() bool {
	return s.name == "BoardConfig" || s.name == "PostConfig" || s.name == "UploadConfig"
}

type fieldType struct {
	composite  string
	name       string
	fType      string
	defaultVal string
	doc        string
}

func docStructs(dir string) (map[string]structType, error) {
	structMap := make(map[string]structType)
	fset := token.NewFileSet()
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		var structName string
		var structDoc string
		file := mustParse(fset, d.Name(), path)

		structDocs := make(map[string]string)

		ast.Inspect(file, func(n ast.Node) bool {
			switch t := n.(type) {
			case *ast.TypeSpec:
				structName = t.Name.String()
				// fmt.Println(structName, "doc:", t)
				if t.Doc == nil {
					structDoc = structDocs[structName]
				} else {
					structDoc = t.Doc.Text()
				}
			case *ast.StructType:
				st := structType{
					name: structName,
					doc:  structDoc,
				}
				for _, field := range t.Fields.List {
					var fieldT fieldType
					if field.Names == nil {
						fieldT.composite = field.Type.(*ast.Ident).Obj.Name
					} else {
						fieldT.name = field.Names[0].String()
					}
					if field.Doc.Text() == "" {
						// field has no documentation, skip it
						continue
					}

					if field.Doc != nil {
						fieldT.doc = field.Doc.Text()
					}
					docLines := strings.Split(fieldT.doc, "\n")

					for _, line := range docLines {
						if strings.HasPrefix(strings.ToLower(line), "default: ") && fieldT.defaultVal == "" {
							fieldT.defaultVal = line[9:]
							break
						}
					}

					switch tt := field.Type.(type) {
					case *ast.Ident:
						fieldT.fType = tt.Name
					case *ast.ArrayType:
						if selectorExpr, ok := tt.Elt.(*ast.SelectorExpr); ok {
							fieldT.fType = "[]" + fmt.Sprintf("%v.%v", selectorExpr.X, selectorExpr.Sel)
						} else {
							fieldT.fType = "[]" + fmt.Sprint(tt.Elt)
						}
					case *ast.MapType:
						fieldT.fType = fmt.Sprintf("map[%v]%v", tt.Key, tt.Value)
					case *ast.StarExpr:
						fieldT.fType = fmt.Sprint(tt.X)
					default:
						panic(fmt.Sprintf("%#v", field.Type))
					}
					st.fields = append(st.fields, fieldT)
				}
				structMap[structName] = st
			case *ast.File:
				// fmt.Println("file name", t.Name)
			case *ast.ImportSpec:
			case *ast.BasicLit:
				// fmt.Println("basiclit:", t.Kind)
			case *ast.ValueSpec:
				// fmt.Println("valuespec:", t)
				if t.Doc != nil {
					fmt.Println("ValueSpec doc:", t.Doc.Text())
				}
			case *ast.StarExpr:
				// fmt.Println("starexpr:", t)
			case *ast.CompositeLit:
				// fmt.Println("compositelit:", t)
			case *ast.MapType:
				// fmt.Println("maptype:", t)
			case *ast.ArrayType:
				// fmt.Println("arraytype:", t)
			case *ast.FieldList:
				// fmt.Println("fieldlist:", t)
			case *ast.Field:
				// fmt.Println("field:", t)
			case *ast.BlockStmt:
				// fmt.Println("blockstmt:", t)
			case *ast.GenDecl:
				doc := t.Doc.Text()
				if doc != "" {
					firstSpace := strings.Index(doc, " ")
					if firstSpace > 0 {
						probableName := doc[:firstSpace]
						structDocs[probableName] = doc
					}
				}
			}
			return true
		})

		return nil
	})
	return structMap, err
}

func fieldsAsMarkdownTable(str *structType, builder *strings.Builder, named bool, showColumnHeaders bool, lengths *columnLengths) {
	if named {
		builder.WriteString("## " + str.name + "\n")
		if str.doc != "" {
			builder.WriteString(str.doc)
		}
	}
	if lengths == nil {
		lengths = &columnLengths{}
		lengths.setLengths(*str)
	}

	if showColumnHeaders {
		builder.WriteString("Field")
		for range lengths.fieldLength - 4 {
			builder.WriteRune(' ')
		}
		builder.WriteString("|Type")
		for range lengths.typeLength - 3 {
			builder.WriteRune(' ')
		}

		if !named {
			builder.WriteString("|Board option ")
		}

		if lengths.defaultLength > 0 {
			builder.WriteString("|Default")
			for range lengths.defaultLength - 4 {
				builder.WriteRune(' ')
			}
		}

		builder.WriteString("|Info\n")
		for range lengths.fieldLength + 1 {
			builder.WriteRune('-')
		}
		builder.WriteRune('|')
		for range lengths.typeLength + 1 {
			builder.WriteRune('-')
		}
		if !named {
			builder.WriteRune('|')
			for range 13 {
				builder.WriteRune('-')
			}
		}
		if lengths.defaultLength > 0 {
			builder.WriteRune('|')
			for range lengths.defaultLength + 3 {
				builder.WriteRune('-')
			}
		}
		builder.WriteString("|--------------\n")
	}

	for _, field := range str.fields {
		if strings.Contains(field.doc, "Deprecated:") {
			continue
		}
		builder.WriteString(field.name)
		for range lengths.fieldLength - len(field.name) + 1 {
			builder.WriteRune(' ')
		}
		builder.WriteRune('|')
		builder.WriteString(field.fType)
		for range lengths.typeLength - len(field.fType) + 1 {
			builder.WriteRune(' ')
		}

		if !named {
			if str.isBoardConfig() {
				builder.WriteString("|Yes          ")
			} else {
				builder.WriteString("|No           ")
			}
		}

		builder.WriteRune('|')
		if lengths.defaultLength > 0 {
			builder.WriteString(field.defaultVal)
			for range lengths.defaultLength - len(field.defaultVal) + 3 {
				builder.WriteRune(' ')
			}
			builder.WriteRune('|')
		}
		builder.WriteString(strings.ReplaceAll(field.doc, "\n", " "))
		builder.WriteRune('\n')
	}
}

func main() {
	if len(os.Args) != 2 {
		fmt.Printf("usage: %s /path/to/gochan/", os.Args[0])
		os.Exit(1)
	}

	gochanRoot := os.Args[1]
	cfgDir := path.Join(gochanRoot, "pkg/config")
	configStructs, err := docStructs(cfgDir)
	if err != nil {
		fmt.Printf("Error parsing package in %s: %s", cfgDir, err)
		os.Exit(1)
	}

	geoipDir := path.Join(gochanRoot, "pkg/posting/geoip")
	geoipStructs, err := docStructs(geoipDir)
	if err != nil {
		fmt.Printf("Error parsing package in %s: %s", geoipDir, err)
		os.Exit(1)
	}

	var builder strings.Builder
	builder.WriteString(configHeader)

	cfgColumnLengths := columnLengths{}
	configStructsArray := make([]structType, 0, len(configStructs))
	for _, str := range compositeStructTypes {
		configStructsArray = append(configStructsArray, configStructs[str])
	}
	cfgColumnLengths.setLengths(configStructsArray...)

	for s, structName := range compositeStructTypes {
		str := configStructs[structName]
		fieldsAsMarkdownTable(&str, &builder, false, s == 0, &cfgColumnLengths)
	}
	builder.WriteString("\nExample options for `GeoIPOptions`:\n")
	builder.WriteString("```JSONC\n" +
		"\"GeoIPType\": \"mmdb\",\n" +
		"\"GeoIPOptions\": {\n" +
		"\t\"dbLocation\": \"/usr/share/geoip/GeoIP2.mmdb\",\n" +
		"\t\"isoCode\": \"en\" // optional\n" +
		"}\n```\n\n" +
		"`CustomFlags` is an array with custom post flags, selectable via dropdown. The `Flag` value is assumed to be a file in /static/flags/. Example:\n" +
		"```JSON\n" +
		"\"CustomFlags\": [\n" +
		"\t{\"Flag\":\"california.png\", \"Name\": \"California\"},\n" +
		"\t{\"Flag\":\"cia.png\", \"Name\": \"CIA\"},\n" +
		"\t{\"Flag\":\"lgbtq.png\", \"Name\": \"LGBTQ\"},\n" +
		"\t{\"Flag\":\"ms-dos.png\", \"Name\": \"MS-DOS\"},\n" +
		"\t{\"Flag\":\"stallman.png\", \"Name\": \"Stallman\"},\n" +
		"\t{\"Flag\":\"templeos.png\", \"Name\": \"TempleOS\"},\n" +
		"\t{\"Flag\":\"tux.png\", \"Name\": \"Linux\"},\n" +
		"\t{\"Flag\":\"windows9x.png\", \"Name\": \"Windows 9x\"}\n" +
		"]\n```\n\n")

	for _, structName := range explicitlyNamedStructTypes {
		str := configStructs[structName]
		if str.name == "" {
			fmt.Println(structName, str)
			continue
		}
		fieldsAsMarkdownTable(&str, &builder, true, true, nil)
		builder.WriteString("\n")
	}

	country := geoipStructs["Country"]
	country.name = "geoip.Country"
	cfgColumnLengths.setLengths(country)
	fieldsAsMarkdownTable(&country, &builder, true, true, &cfgColumnLengths)
	fmt.Println(builder.String())
}
