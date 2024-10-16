package define

import (
	"fmt"
	"sync"

	"github.com/spf13/pflag"
)

type discoveredTest namedItem[Requirements]
type discoveredGroup namedItem[map[string][]*discoveredTest]
type discoveredPlatform struct {
	namedItem[map[OS][]*discoveredGroup]
}

type namedItem[T any] struct {
	Name string `json:"name,omitempty" yaml:"name,omitempty"`
	Item T      `json:"item,omitempty" yaml:"item,omitempty"`
}

var (
	DryRun    bool
	Groups    []string
	Platforms []string

	discoveredTests map[string]*discoveredPlatform
	mx              sync.Mutex
)

func RegisterFlags(prefix string, set *pflag.FlagSet) {
	set.BoolVar(&DryRun, prefix+"dry-run", false, "Forces test in dry-run mode: drops platform/group/sudo requirements")
	set.StringArrayVar(&Groups, prefix+"groups", nil, "test groups")
	set.StringArrayVar(&Platforms, prefix+"plarforms", nil, "test platforms")
}

func discoverTest(name string, reqs Requirements) {
	mx.Lock()
	defer mx.Unlock()

	dTest := &discoveredTest{
		Name: name,
		Item: reqs,
	}

	for _, cOS := range reqs.OS {
		archs := []string{AMD64, ARM64}

		if cOS.Arch != "" {
			archs = []string{cOS.Arch}
		}

		for _, arch := range archs {
			platform := fmt.Sprintf("%s/%s", cOS.Type, arch)

			//get the OSes for the platform
			dPlatform, ok := discoveredTests[platform]
			if !ok {
				dPlatform = &discoveredPlatform{
					namedItem: namedItem[map[OS][]*discoveredGroup]{
						Name: platform,
						Item: map[OS][]*discoveredGroup{},
					},
				}
				discoveredTests[platform] = dPlatform
			}

			//get the platform os
			dOS, ok := dPlatform.Item[cOS]
			if !ok {
				dPlatform.Item[cOS] = []*discoveredGroup{}
			}

			//// check the group
			//dGroup, ok := dOS.Item[group]
			//if !ok {
			//	dGroup = []*discoveredGroup{
			//		{
			//			Name: group,
			//			Item: map[string][]*discoveredTest{name: {dTest}},
			//		},
			//	}
			//	dPlatform.Item[group] = dGroup
			//}
		}

	}
}
