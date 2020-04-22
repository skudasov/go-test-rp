package testmodule

import (
	"fmt"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered in main", r)
		}
	}()
	exitVal := m.Run()
	fmt.Printf("exit val: %d\n", exitVal)
	os.Exit(exitVal)
}

func TestRootModuleDummy(t *testing.T) {
	panic("some panic")
	t.Log("root module log")
	t.Fail()
}
