package webapp

import (
	"errors"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/asciimoo/omnom/config"
	"github.com/asciimoo/omnom/model"

	"github.com/gin-gonic/gin"

	"github.com/gin-contrib/multitemplate"
	"github.com/gin-gonic/contrib/sessions"
)

const (
	SERVER_ADDR string = ":7331"
	SID         string = "sid"
)

var e *gin.Engine
var baseURL func(string) string

var tplFuncMap = template.FuncMap{
	"HasPrefix": strings.HasPrefix,
	"ToHTML":    func(s string) template.HTML { return template.HTML(s) },
	"ToAttr":    func(s string) template.HTMLAttr { return template.HTMLAttr(s) },
	"ToURL":     func(s string) template.URL { return template.URL(s) },
	"ToDate":    func(t time.Time) string { return t.Format("2006-01-02") },
	"inc":       func(i int64) int64 { return i + 1 },
	"dec":       func(i int64) int64 { return i - 1 },
	"Truncate": func(s string, maxLen int) string {
		if len(s) > maxLen {
			return s[:maxLen] + "[..]"
		} else {
			return s
		}
	},
	"KVData": func(values ...interface{}) (map[string]interface{}, error) {
		if len(values)%2 != 0 {
			return nil, errors.New("invalid dict call")
		}
		dict := make(map[string]interface{}, len(values)/2)
		for i := 0; i < len(values); i += 2 {
			key, ok := values[i].(string)
			if !ok {
				return nil, errors.New("dict keys must be strings")
			}
			dict[key] = values[i+1]
		}
		return dict, nil
	},
}

var bookmarksPerPage int64 = 20

func createRenderer() multitemplate.Renderer {
	r := multitemplate.DynamicRender{}
	r.AddFromFilesFuncs("index", tplFuncMap, "templates/layout/base.tpl", "templates/index.tpl")
	r.AddFromFilesFuncs("dashboard", tplFuncMap, "templates/layout/base.tpl", "templates/dashboard.tpl")
	r.AddFromFilesFuncs("signup", tplFuncMap, "templates/layout/base.tpl", "templates/signup.tpl")
	r.AddFromFilesFuncs("signup-confirm", tplFuncMap, "templates/layout/base.tpl", "templates/signup_confirm.tpl")
	r.AddFromFilesFuncs("login", tplFuncMap, "templates/layout/base.tpl", "templates/login.tpl")
	r.AddFromFilesFuncs("login-confirm", tplFuncMap, "templates/layout/base.tpl", "templates/login_confirm.tpl")
	r.AddFromFilesFuncs("bookmarks", tplFuncMap, "templates/layout/base.tpl", "templates/bookmarks.tpl")
	r.AddFromFilesFuncs("my-bookmarks", tplFuncMap, "templates/layout/base.tpl", "templates/my_bookmarks.tpl")
	r.AddFromFilesFuncs("profile", tplFuncMap, "templates/layout/base.tpl", "templates/profile.tpl")
	r.AddFromFilesFuncs("snapshotWrapper", tplFuncMap, "templates/layout/base.tpl", "templates/snapshot_wrapper.tpl")
	r.AddFromFilesFuncs("view-bookmark", tplFuncMap, "templates/layout/base.tpl", "templates/view_bookmark.tpl")
	r.AddFromFilesFuncs("edit-bookmark", tplFuncMap, "templates/layout/base.tpl", "templates/edit_bookmark.tpl")
	r.AddFromFilesFuncs("api", tplFuncMap, "templates/layout/base.tpl", "templates/api.tpl")
	return r
}

func renderHTML(c *gin.Context, status int, page string, vars map[string]interface{}) {
	session := sessions.Default(c)
	u, _ := c.Get("user")
	tplVars := gin.H{
		"Page": page,
		"User": u,
	}
	sessChanged := false
	if s := session.Get("Error"); s != nil {
		tplVars["Error"] = s.(string)
		session.Delete("Error")
		sessChanged = true
	}
	if s := session.Get("Warning"); s != nil {
		tplVars["Warning"] = s.(string)
		session.Delete("Warning")
		sessChanged = true
	}
	if s := session.Get("Info"); s != nil {
		tplVars["Info"] = s.(string)
		session.Delete("Info")
		sessChanged = true
	}
	if sessChanged {
		session.Save()
	}
	for k, v := range vars {
		tplVars[k] = v
	}
	c.HTML(status, page, tplVars)
}

func registerEndpoint(r *gin.RouterGroup, e *Endpoint) {
	switch e.Method {
	case GET:
		r.GET(e.Path, e.Handler)
	case POST:
		r.POST(e.Path, e.Handler)
	case PUT:
		r.PUT(e.Path, e.Handler)
	case PATCH:
		r.PATCH(e.Path, e.Handler)
	case HEAD:
		r.HEAD(e.Path, e.Handler)
	}
}

func Run(cfg *config.Config) {
	e = gin.Default()
	if !cfg.App.Debug {
		gin.SetMode(gin.ReleaseMode)
	}
	if cfg.App.BookmarksPerPage > 0 {
		bookmarksPerPage = cfg.App.BookmarksPerPage
	}
	e.SetTrustedProxies([]string{"127.0.0.1"})
	e.Use(sessions.Sessions("SID", sessions.NewCookieStore([]byte("secret"))))
	e.Use(SessionMiddleware())
	e.Use(ConfigMiddleware(cfg))
	authorized := e.Group("/")
	authorized.Use(authRequired)

	bu := cfg.Server.BaseURL
	baseURL = func(u string) string {
		if strings.HasPrefix(u, "/") && strings.HasSuffix(bu, "/") {
			u = u[1:]
		}
		return bu + u
	}
	tplFuncMap["BaseURL"] = baseURL
	e.HTMLRender = createRenderer()

	// ROUTES
	e.Static("/static", "./static")
	for _, ep := range Endpoints {
		if ep.AuthRequired {
			registerEndpoint(authorized, ep)
		} else {
			registerEndpoint(&e.RouterGroup, ep)
		}
	}

	log.Println("Starting server")
	e.Run(cfg.Server.Address)
}

func index(c *gin.Context) {
	if u, ok := c.Get("user"); ok && u != nil {
		dashboard(c, u.(*model.User))
		return
	}
	renderHTML(c, http.StatusOK, "index", nil)
}

func authRequired(c *gin.Context) {
	session := sessions.Default(c)
	user := session.Get(SID)
	if user == nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
			"error": "unauthorized",
		})
		return
	}
	c.Next()
}

func getPageno(c *gin.Context) int64 {
	var pageno int64 = 1
	if pagenoStr, ok := c.GetQuery("pageno"); ok {
		if userPageno, err := strconv.Atoi(pagenoStr); err == nil && userPageno > 0 {
			pageno = int64(userPageno)
		}
	}
	return pageno
}

func SessionMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		uname := session.Get(SID)
		if uname != nil {
			c.Set("user", model.GetUser(uname.(string)))
		} else {
			c.Set("user", nil)
		}
		c.Next()
	}
}

func ConfigMiddleware(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("config", cfg)
		c.Next()
	}
}
