package gateway

import (
	"testing"

	"github.com/rivine/rivine/types"
)

func TestLoad(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()
	g := newTestingGateway(t)

	g.mu.Lock()
	g.addNode(dummyNode)
	g.save()
	g.mu.Unlock()
	g.Close()

	g2, err := New("localhost:0", false, g.persistDir, types.DefaultBlockchainInfo(), types.DefaultChainConstants(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := g2.nodes[dummyNode]; !ok {
		t.Fatal("gateway did not load old peer list:", g2.nodes)
	}
}
