package submodule1

import (
	"testing"
	"time"
)

func TestDummy1(t *testing.T) {
	t.Run("subtest dummy 1", func(t *testing.T) {
		t.Parallel()
		time.Sleep(1 * time.Second)
		t.Log("C11 abc")
		t.Log("https://insolar.atlassian.net/browse/MN-1")
		t.Skip()
	})
	t.Run("subtest dummy 2", func(t *testing.T) {
		t.Parallel()
		time.Sleep(2 * time.Second)
		t.Log("C12 abc")
	})
}
