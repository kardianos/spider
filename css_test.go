package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/tdewolff/parse/css"
)

func TestCss(t *testing.T) {
	sr := strings.NewReader(stylesheet)
	parser := css.NewParser(sr, true)
	for {
		gr, _, _ := parser.Next()
		if gr == css.ErrorGrammar {
			break
		}
		vals := parser.Values()
		for _, v := range vals {
			if v.TokenType == css.URLToken {
				var us = v.Data
				us = bytes.TrimPrefix(us, []byte("url("))
				us = bytes.TrimSuffix(us, []byte(")"))
				us = bytes.Trim(us, `"'`)
				fmt.Printf("%v %s -> %s\n", v.TokenType, v.Data, us)

			}
		}
	}
}

var stylesheet = `
body {
	font-size: 14pt;
	background: url("a.gif") no-repeat -9999px -9999px;
}
.navcat a {
	background: transparent url("ra.gif") no-repeat scroll right center;
	min-height: 44px;
	line-height: 44px;
	padding-left: 15px;
}

.navcat a[selected] {
	background: #fff url('la.gif') no-repeat scroll right center;
}
.navcat2 a[selected] {
	background: #fff url(ps.gif) no-repeat scroll right center;
}
`
