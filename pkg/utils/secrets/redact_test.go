package secrets

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func Test_walkNode(t *testing.T) {
	type args struct {
		yamlInput string
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "easy mapped struct",
			args: args{
				yamlInput: `
                    s1: v1
                    s2: v2
                    i1: 42
                    b1: true
                    `,
			},
			wantErr: false,
		},
		{
			name: "nested mapped struct",
			args: args{
				yamlInput: `
                    n1:
                        s1: v1
                        s2: v2
                    n2:
                        i1: 42
                    b1: true
                    `,
			},
			wantErr: false,
		},
		{
			name: "nested mapped struct and sequences",
			args: args{
				yamlInput: `
                    n1:
                        s1: v1
                        s2: v2
                    n2:
                        fibonacci:
                            - 0
                            - 1
                            - 2
                            - 3
                            - 5
                            - 8
                            - 13
                            - twenty-one
                    b1: true
                    `,
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var unmarshalled yaml.Node
			decoder := yaml.NewDecoder(strings.NewReader(tt.args.yamlInput))
			err := decoder.Decode(&unmarshalled)
			require.NoError(t, err)
			if err := walkNode(&unmarshalled, nil); (err != nil) != tt.wantErr {
				t.Errorf("walkNode() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
