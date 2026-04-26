package pgxcel

import (
	"testing"

	"github.com/google/cel-go/cel"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestPgxcel(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "pgxcel Suite")
}

func mustCompile(src string, opts ...cel.EnvOption) *cel.Ast {
	GinkgoHelper()
	env, err := cel.NewEnv(opts...)
	Expect(err).NotTo(HaveOccurred())
	ast, iss := env.Compile(src)
	Expect(iss.Err()).NotTo(HaveOccurred())
	return ast
}
