package daemon

import "testing"

func TestTheProbeTimeoutIsFiveSeconds(t *testing.T) {
	t.Parallel()
	if daemonVersionProbeTimeout.Seconds() != 5 {
		t.Errorf("timeout = %v", daemonVersionProbeTimeout)
	}
}
