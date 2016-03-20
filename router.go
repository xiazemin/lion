package lion

import (
	"fmt"
	"net/http"
	"os"
	"sync"

	"golang.org/x/net/context"
)

// Router is responsible for registering handlers and middlewares
type Router struct {
	tree *tree
	rm   RegisterMatcher

	router *Router

	middlewares Middlewares

	handler Handler // TODO: create a handler

	pattern string

	notFoundHandler Handler

	registeredHandlers []registeredHandler // Used for Mount()

	pool sync.Pool

	namedMiddlewares map[string]Middlewares
}

// New creates a new router instance
func New(mws ...Middleware) *Router {
	r := &Router{
		middlewares:      Middlewares{},
		rm:               newRadixMatcher(),
		namedMiddlewares: make(map[string]Middlewares),
	}
	r.pool.New = func() interface{} {
		return NewContext()
	}
	r.router = r
	r.Use(mws...)
	return r
}

// Group creates a subrouter with parent pattern provided.
func (r *Router) Group(pattern string, mws ...Middleware) *Router {
	p := r.pattern + pattern
	if pattern == "/" && r.pattern != "/" {
		p = r.pattern
	}
	validatePattern(p)

	nr := &Router{
		router:           r,
		rm:               r.rm,
		pattern:          p,
		middlewares:      Middlewares{},
		namedMiddlewares: make(map[string]Middlewares),
	}
	nr.Use(mws...)
	return nr
}

// Get registers an http GET method receiver with the provided Handler
func (r *Router) Get(pattern string, handler Handler) {
	r.Handle("GET", pattern, handler)
}

// Post registers an http POST method receiver with the provided Handler
func (r *Router) Post(pattern string, handler Handler) {
	r.Handle("POST", pattern, handler)
}

// Put registers an http PUT method receiver with the provided Handler
func (r *Router) Put(pattern string, handler Handler) {
	r.Handle("PUT", pattern, handler)
}

// Patch registers an http PATCH method receiver with the provided Handler
func (r *Router) Patch(pattern string, handler Handler) {
	r.Handle("PATCH", pattern, handler)
}

// Delete registers an http DELETE method receiver with the provided Handler
func (r *Router) Delete(pattern string, handler Handler) {
	r.Handle("DELETE", pattern, handler)
}

// GetFunc wraps a HandlerFunc as a Handler and registers it to the router
func (r *Router) GetFunc(pattern string, fn HandlerFunc) {
	r.Get(pattern, HandlerFunc(fn))
}

// PostFunc wraps a HandlerFunc as a Handler and registers it to the router
func (r *Router) PostFunc(pattern string, fn HandlerFunc) {
	r.Post(pattern, HandlerFunc(fn))
}

// PutFunc wraps a HandlerFunc as a Handler and registers it to the router
func (r *Router) PutFunc(pattern string, fn HandlerFunc) {
	r.Put(pattern, HandlerFunc(fn))
}

// PatchFunc wraps a HandlerFunc as a Handler and registers it to the router
func (r *Router) PatchFunc(pattern string, fn HandlerFunc) {
	r.Patch(pattern, HandlerFunc(fn))
}

// DeleteFunc wraps a HandlerFunc as a Handler and registers it to the router
func (r *Router) DeleteFunc(pattern string, fn HandlerFunc) {
	r.Delete(pattern, HandlerFunc(fn))
}

// Use registers middlewares to be used
func (r *Router) Use(middlewares ...Middleware) {
	r.middlewares = append(r.middlewares, middlewares...)
}

// UseFunc wraps a MiddlewareFunc as a Middleware and registers it middlewares to be used
func (r *Router) UseFunc(middlewareFuncs ...MiddlewareFunc) {
	for _, fn := range middlewareFuncs {
		r.Use(MiddlewareFunc(fn))
	}
}

type negroniHandler interface {
	ServeHTTP(rw http.ResponseWriter, r *http.Request, next http.HandlerFunc)
}

type negroniHandlerFunc func(rw http.ResponseWriter, r *http.Request, next http.HandlerFunc)

func (h negroniHandlerFunc) ServeHTTP(rw http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	h(rw, r, next)
}

func (r *Router) UseNegroni(n negroniHandler) {
	r.Use(MiddlewareFunc(func(next Handler) Handler {
		return HandlerFunc(func(c context.Context, w http.ResponseWriter, r *http.Request) {
			n.ServeHTTP(w, r, HandlerFunc(func(_ context.Context, w http.ResponseWriter, r *http.Request) {
				next.ServeHTTPC(c, w, r)
			}).ServeHTTP)
		})
	}))
}

func (r *Router) UseNegroniFunc(n func(rw http.ResponseWriter, r *http.Request, next http.HandlerFunc)) {
	r.UseNegroni(negroniHandlerFunc(n))
}

// UseHandler uses
func (r *Router) UseHandler(handler Handler) {
	r.UseFunc(func(next Handler) Handler {
		return HandlerFunc(func(c context.Context, w http.ResponseWriter, r *http.Request) {
			handler.ServeHTTPC(c, w, r)
			next.ServeHTTPC(c, w, r)
		})
	})
}

// UseHandlerFunc uses
func (r *Router) UseHandlerFunc(fn HandlerFunc) {
	r.UseHandler(HandlerFunc(fn))
}

// Handle is the underling method responsible for registering a handler for a specific method and pattern.
func (r *Router) Handle(method, pattern string, handler Handler) {
	validatePattern(pattern)

	var p string
	if !r.isRoot() && pattern == "/" {
		p = r.pattern
	} else {
		p = r.pattern + pattern
	}

	built := r.buildMiddlewares(handler)
	r.registeredHandlers = append(r.registeredHandlers, registeredHandler{method, pattern, built})
	r.router.rm.Register(method, p, built)
}

type registeredHandler struct {
	method, pattern string
	handler         Handler
}

// Mount mounts a subrouter at the provided pattern
func (r *Router) Mount(pattern string, router *Router, mws ...Middleware) {
	sub := r.Group(pattern, mws...)
	for _, rh := range router.registeredHandlers {
		sub.Handle(rh.method, rh.pattern, rh.handler)
	}
}

func (r *Router) buildMiddlewares(handler Handler) Handler {
	handler = r.middlewares.BuildHandler(handler)
	if !r.isRoot() {
		handler = r.router.buildMiddlewares(handler)
	}
	return handler
}

func (r *Router) isRoot() bool {
	return r.router == r
}

// HandleFunc wraps a HandlerFunc and pass it to Handle method
func (r *Router) HandleFunc(method, pattern string, fn HandlerFunc) {
	r.Handle(method, pattern, HandlerFunc(fn))
}

// ServeHTTP calls ServeHTTPC with a context.Background()
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.ServeHTTPC(context.TODO(), w, req)
}

// ServeHTTPC finds the handler associated with the request's path.
// If it is not found it calls the NotFound handler
func (r *Router) ServeHTTPC(c context.Context, w http.ResponseWriter, req *http.Request) {
	ctx := r.pool.Get().(*Context)
	ctx.parent = c

	if ctx, h := r.router.rm.Match(ctx, req); h != nil {
		h.ServeHTTPC(ctx, w, req)
	} else {
		r.notFound(ctx, w, req) // r.middlewares.BuildHandler(HandlerFunc(r.NotFound)).ServeHTTPC
	}

	ctx.reset()
	r.pool.Put(ctx)
}

// NotFound calls NotFoundHandler() if it is set. Otherwise, it calls net/http.NotFound
func (r *Router) notFound(c context.Context, w http.ResponseWriter, req *http.Request) {
	if r.router.notFoundHandler != nil {
		r.router.notFoundHandler.ServeHTTPC(c, w, req)
	} else {
		http.NotFound(w, req)
	}
}

func (r *Router) NotFoundHandler(handler Handler) {
	r.notFoundHandler = handler
}

// ServeFiles serves files located in root http.FileSystem
//
// This can be used as shown below:
// 	r := New()
// 	r.ServeFiles("/static", http.Dir("static")) // This will serve files in the directory static with /static prefix
func (r *Router) ServeFiles(path string, root http.FileSystem) {
	fileServer := http.FileServer(root)
	r.Get(path, HandlerFunc(func(c context.Context, w http.ResponseWriter, r *http.Request) {
		fmt.Println("rurl", r.URL.Path)
		fileServer.ServeHTTP(w, r)
	}))
}

// GetH wraps a http.Handler
func (r *Router) GetH(pattern string, handler http.Handler) {
	r.Get(pattern, Wrap(handler))
}

// PostH wraps a http.Handler
func (r *Router) PostH(pattern string, handler http.Handler) {
	r.Post(pattern, Wrap(handler))
}

// PutH wraps a http.Handler
func (r *Router) PutH(pattern string, handler http.Handler) {
	r.Put(pattern, Wrap(handler))
}

// DeleteH wraps a http.Handler
func (r *Router) DeleteH(pattern string, handler http.Handler) {
	r.Delete(pattern, Wrap(handler))
}

// Run calls http.ListenAndServe for the current router.
// If no addresses are specified as arguments, it will use the PORT environnement variable if it is defined. Otherwise, it will listen on port 3000 of the localmachine
//
// 	r := New()
// 	r.Run() // will call
// 	r.Run(":8080")
func (r *Router) Run(addr ...string) {
	var a string

	if len(addr) == 0 {
		if p := os.Getenv("PORT"); p != "" {
			a = p
		} else {
			a = ":3000"
		}
	} else {
		a = addr[0]
	}

	lionLogger.Printf("listening on %s", a)
	lionLogger.Fatal(http.ListenAndServe(a, r))
}

// RunTLS calls http.ListenAndServeTLS for the current router
//
// 	r := New()
// 	r.RunTLS(":3443", "cert.pem", "key.pem")
func (r *Router) RunTLS(addr, certFile, keyFile string) {
	lionLogger.Printf("listening on %s", addr)
	lionLogger.Fatal(http.ListenAndServeTLS(addr, certFile, keyFile, r))
}

func (r *Router) Define(name string, mws ...Middleware) {
	r.namedMiddlewares[name] = append(r.namedMiddlewares[name], mws...)
}

func (r *Router) DefineFunc(name string, mws ...MiddlewareFunc) {
	for _, mw := range mws {
		r.Define(name, mw)
	}
}

func (r *Router) UseNamed(name string) {
	if r.hasNamed(name) { // Find if it this is registered in the current router
		r.Use(r.namedMiddlewares[name]...)
	} else if !r.isRoot() { // Otherwise, look for it in parent router.
		r.router.UseNamed(name)
	} else { // not found
		panic("Unknow named middlewares: " + name)
	}
}

func (r *Router) hasNamed(name string) bool {
	_, exist := r.namedMiddlewares[name]
	return exist
}

func validatePattern(pattern string) {
	if len(pattern) > 0 && pattern[0] != '/' {
		panic("path must start with '/' in path '" + pattern + "'")
	}
}