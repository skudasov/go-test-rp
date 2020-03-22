package testchild

import (
	"testing"
	"time"
)

func TestDummy2(t *testing.T) {
	t.Run("subtest dummy 1", func(t *testing.T) {
		t.Parallel()
		time.Sleep(3 * time.Second)
		t.Log("C21 abc")
		t.Fail()
	})
	t.Run("subtest dummy 2", func(t *testing.T) {
		t.Parallel()
		time.Sleep(4 * time.Second)
		t.Log("C22 abc")
	})
}

func TestSingle(t *testing.T) {
	time.Sleep(5 * time.Second)
	t.Log("C23")
}
