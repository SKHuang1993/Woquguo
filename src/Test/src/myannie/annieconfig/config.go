package annieconfig

var (

	// Debug debug mode
	Debug bool
	// Version show version
	Version bool
	// InfoOnly Information only mode
	InfoOnly bool

	// Cookie http cookies

	Cookie string

	// Playlist download playlist

	Playlist bool

	// Proxy HTTP proxy
	Proxy string

	// Refer use specified Referrer
	Refer string

	// Socks5Proxy SOCKS5 proxy
	SocksProxy    string
	Format        string
	OutputPath    string
	OutputName    string
	ExtractedData bool
)

//

var FakeHeaders = map[string]string{

	"Accept-Language": "en-US,en;q=0.8", //
	"Accept-Encoding": "gzip,deflate,sdch",
	"Accept-Charset":  "UTF-8,*;q=0.5",
	"Accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
	"User-Agent":      "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_13_3) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/65.0.3325.146 Safari/537.36",
}
