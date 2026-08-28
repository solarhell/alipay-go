package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strings"
)

// prune 删除生成代码中的 *DefaultResponse 类型及其方法，并摘掉随之失效的 import。
//
// 这些类型是 spec 里 default response（也就是错误响应）的 union 包装，本 SDK
// 不用它们——错误由 error.go 解析成 *Error。留着它们的唯一后果是 12 处
// runtime.JSONMerge 调用，把 go-jsonmerge、uuid 一并拖进依赖树。支付 SDK 的
// 依赖面就是攻击面，为一段死代码扩大它不划算，所以在这里剪掉，让本 SDK 只依赖
// 标准库。
//
// 万一将来某个在用的类型引用了被删的类型，编译会立刻失败——这正是想要的：
// 剪枝出了错要当场知道，而不是留一个静悄悄的错误产物。
func prune(f *ast.File, fset *token.FileSet, src []byte) ([]byte, int, error) {
	type span struct{ start, end int }
	var cuts []span
	removed := 0

	cut := func(node ast.Node, doc *ast.CommentGroup) {
		start := node.Pos()
		if doc != nil {
			start = doc.Pos()
		}
		cuts = append(cuts, span{fset.Position(start).Offset, fset.Position(node.End()).Offset})
	}

	isDead := func(name string) bool { return strings.HasSuffix(name, "DefaultResponse") }

	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil || len(d.Recv.List) == 0 {
				continue
			}
			typ := d.Recv.List[0].Type
			if star, ok := typ.(*ast.StarExpr); ok {
				typ = star.X
			}
			if ident, ok := typ.(*ast.Ident); ok && isDead(ident.Name) {
				cut(d, d.Doc)
				removed++
			}
		case *ast.GenDecl:
			if d.Tok != token.TYPE {
				continue
			}
			// 生成的代码里类型各自成块，整块删除即可；真出现混合块就报错，
			// 而不是删掉一半留下语法残骸。
			dead, live := 0, 0
			for _, spec := range d.Specs {
				if ts, ok := spec.(*ast.TypeSpec); ok && isDead(ts.Name.Name) {
					dead++
				} else {
					live++
				}
			}
			if dead > 0 && live > 0 {
				return nil, 0, fmt.Errorf("%s: 待删类型与保留类型同处一个声明块，剪枝逻辑需要修订",
					fset.Position(d.Pos()))
			}
			if dead > 0 {
				cut(d, d.Doc)
				removed += dead
			}
		}
	}

	sort.Slice(cuts, func(i, j int) bool { return cuts[i].start > cuts[j].start })
	out := src
	for _, c := range cuts {
		out = append(out[:c.start:c.start], out[c.end:]...)
	}

	out, err := dropUnusedImports(out)
	if err != nil {
		return nil, 0, err
	}
	return out, removed, nil
}

// dropUnusedImports 摘掉剪枝后不再被引用的 import。
//
// 不硬编码包名：删掉 DefaultResponse 之后失效的恰好是 runtime 和 encoding/json，
// 但白名单一变，失效的集合也会变，所以这里照着剩下的代码实际用了什么来判断。
func dropUnusedImports(src []byte) ([]byte, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("重新解析剪枝结果: %w", err)
	}

	used := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if ident, ok := sel.X.(*ast.Ident); ok {
				used[ident.Name] = true
			}
		}
		return true
	})

	var cuts [][2]int
	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.IMPORT {
			continue
		}
		dead := 0
		for _, spec := range gen.Specs {
			imp := spec.(*ast.ImportSpec)
			name := strings.Trim(imp.Path.Value, `"`)
			if imp.Name != nil {
				name = imp.Name.Name
			} else if i := strings.LastIndex(name, "/"); i >= 0 {
				name = name[i+1:]
			}
			if !used[name] {
				dead++
				cuts = append(cuts, [2]int{fset.Position(imp.Pos()).Offset, fset.Position(imp.End()).Offset})
			}
		}
		// import 块被清空时连块一起删，免得留下一个空的 import ()。
		if dead == len(gen.Specs) {
			cuts = cuts[:len(cuts)-dead]
			cuts = append(cuts, [2]int{fset.Position(gen.Pos()).Offset, fset.Position(gen.End()).Offset})
		}
	}

	sort.Slice(cuts, func(i, j int) bool { return cuts[i][0] > cuts[j][0] })
	for _, c := range cuts {
		src = append(src[:c[0]:c[0]], src[c[1]:]...)
	}
	return src, nil
}

func writeFile(path string, data []byte) error { return os.WriteFile(path, data, 0o644) }
