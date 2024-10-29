package secrets

import (
	"errors"
	"fmt"
	"log"
	"strconv"

	"gopkg.in/yaml.v3"
)

var UnsupportedNodeErr = errors.New("unsupported node type")

func walkNode(n *yaml.Node, path []string) error {
	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) == 1 {
			return walkNode(n.Content[0], nil)
		}
		return fmt.Errorf("found document node type with 0 or multiple content(s): %w", UnsupportedNodeErr)
	case yaml.MappingNode:
		mappingsNo := len(n.Content)
		for i := 0; i < mappingsNo; i += 2 {
			key := n.Content[i].Value
			value := n.Content[i+1]
			nestedErr := walkNode(value, append(path, key))
			if nestedErr != nil {
				return nestedErr
			}
		}
		return nil
	case yaml.SequenceNode:
		seqLength := len(n.Content)
		for i := 0; i < seqLength; i++ {
			value := n.Content[i]
			nestedErr := walkNode(value, append(path, strconv.Itoa(i)))
			if nestedErr != nil {
				return nestedErr
			}
		}
		return nil
	case yaml.ScalarNode:
		log.Printf("Found scalar %s (%s): %v", path, n.ShortTag(), n.Value)
		return nil
	case yaml.AliasNode:
		return fmt.Errorf("walking <alias> node type line: %d col: %d: %w", n.Line, n.Column, UnsupportedNodeErr)
	default:
		return fmt.Errorf("walking unknown %d node type line: %d col: %d : %w", n.Kind, n.Line, n.Column, UnsupportedNodeErr)
	}
}
