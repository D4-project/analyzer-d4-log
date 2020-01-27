package logparser

type (
	// Parser provides the interface for a Parser
	// It should provide:
	//  Parse to parse a line of log
	//  GetAttributes to get list of attributes (map keys)
	Parser interface {
		Parse(string) error
		Push() error
		Pop() map[string]string
	}
)
