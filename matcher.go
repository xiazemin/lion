package lion

import (
	"net/http"
	"sync"

	"github.com/celrenheit/lion/matcher"
)

// RegisterMatcher registers and matches routes to Handlers
type RegisterMatcher interface {
	Register(method, pattern string, handler Handler)
	Match(*Context, *http.Request) (*Context, Handler)
}

////////////////////////////////////////////////////////////////////////////
///												RADIX 																				 ///
////////////////////////////////////////////////////////////////////////////

var _ RegisterMatcher = (*pathMatcher)(nil)

type pathMatcher struct {
	matcher  matcher.Matcher
	tagsPool sync.Pool
}

func newPathMatcher() *pathMatcher {
	cfg := &matcher.Config{
		ParamChar:        ':',
		WildcardChar:     '*',
		Separators:       "/.",
		GetSetterCreator: &creator{},
	}

	r := &pathMatcher{
		matcher: matcher.Custom(cfg),
	}
	return r
}

func (d *pathMatcher) Register(method, pattern string, handler Handler) {
	d.prevalidation(method, pattern)

	d.matcher.Set(pattern, handler, matcher.Tags{method})
}

func (d *pathMatcher) Match(c *Context, r *http.Request) (*Context, Handler) {
	p := cleanPath(r.URL.Path)

	ti := grabTagsItem()
	ti.tags = append(ti.tags, r.Method)

	h := d.matcher.GetWithContext(c, p, ti.tags)

	putTagsItem(ti)

	if handler, ok := h.(Handler); ok {
		return c, handler
	}

	return c, nil
}

func (d *pathMatcher) prevalidation(method, pattern string) {
	if len(pattern) == 0 || pattern[0] != '/' {
		panicl("path must begin with '/' in path '" + pattern + "'")
	}

	// Is http method allowed
	if !isInStringSlice(allowedHTTPMethods[:], method) {
		panicl("lion: invalid http method => %s\n\tShould be one of %v", method, allowedHTTPMethods)
	}
}

func isInStringSlice(slice []string, expected string) bool {
	for _, val := range slice {
		if val == expected {
			return true
		}
	}
	return false
}

type methodsHandlers struct {
	get     Handler
	head    Handler
	post    Handler
	put     Handler
	delete  Handler
	trace   Handler
	options Handler
	connect Handler
	patch   Handler
}

func (gs *methodsHandlers) Set(value interface{}, tags matcher.Tags) {
	if len(tags) != 1 {
		panicl("Length != 1")
	}

	method := tags[0]

	var handler Handler
	if value == nil {
		handler = nil
	} else {
		if h, ok := value.(Handler); !ok {
			panicl("Not handler")
		} else {
			handler = h
		}
	}

	gs.addHandler(method, handler)
}

func (gs *methodsHandlers) Get(tags matcher.Tags) interface{} {
	if len(tags) != 1 {
		panicl("No method")
	}

	method := tags[0]

	return gs.getHandler(method)
}

func (gs *methodsHandlers) addHandler(method string, handler Handler) {
	switch method {
	case GET:
		gs.get = handler
	case HEAD:
		gs.head = handler
	case POST:
		gs.post = handler
	case PUT:
		gs.put = handler
	case DELETE:
		gs.delete = handler
	case TRACE:
		gs.trace = handler
	case OPTIONS:
		gs.options = handler
	case CONNECT:
		gs.connect = handler
	case PATCH:
		gs.patch = handler
	}
}

func (gs *methodsHandlers) getHandler(method string) Handler {
	switch method {
	case GET:
		return gs.get
	case HEAD:
		return gs.head
	case POST:
		return gs.post
	case PUT:
		return gs.put
	case DELETE:
		return gs.delete
	case TRACE:
		return gs.trace
	case OPTIONS:
		return gs.options
	case CONNECT:
		return gs.connect
	case PATCH:
		return gs.patch
	default:
		return nil
	}
}

type creator struct{}

func (c *creator) New() matcher.GetSetter {
	return &methodsHandlers{}
}

//// Tags Item
type tagsItem struct {
	tags matcher.Tags
}

func (ti *tagsItem) reset() {
	ti.tags = ti.tags[:0]
}

var tagsItemPool = sync.Pool{
	New: func() interface{} {
		return &tagsItem{}
	},
}

func grabTagsItem() *tagsItem {
	return tagsItemPool.Get().(*tagsItem)
}

func putTagsItem(ti *tagsItem) {

	ti.reset()
	tagsItemPool.Put(ti)
}
