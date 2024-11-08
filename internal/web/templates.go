package web

import (
	"bytes"
	"html/template"
	"path"
	"reflect"

	depUtil "github.com/glasskube/glasskube/internal/dependency/util"

	webutil "github.com/glasskube/glasskube/internal/web/sse/refresh"

	"github.com/fsnotify/fsnotify"
	"github.com/glasskube/glasskube/api/v1alpha1"
	"github.com/glasskube/glasskube/internal/controller/ctrlpkg"
	repoclient "github.com/glasskube/glasskube/internal/repo/client"
	"github.com/glasskube/glasskube/internal/semver"
	"github.com/glasskube/glasskube/internal/web/components/datalist"
	"github.com/glasskube/glasskube/internal/web/components/pkg_config_input"
	"github.com/glasskube/glasskube/internal/web/components/pkg_detail_btns"
	"github.com/glasskube/glasskube/internal/web/components/pkg_overview_btn"
	"github.com/glasskube/glasskube/internal/web/components/pkg_update_alert"
	"github.com/glasskube/glasskube/internal/web/components/toast"
	"github.com/glasskube/glasskube/pkg/condition"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
	"go.uber.org/multierr"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type templates struct {
	templateFuncs           template.FuncMap
	baseTemplate            *template.Template
	clusterPkgsPageTemplate *template.Template
	pkgsPageTmpl            *template.Template
	pkgPageTmpl             *template.Template
	pkgDiscussionPageTmpl   *template.Template
	supportPageTmpl         *template.Template
	bootstrapPageTmpl       *template.Template
	kubeconfigPageTmpl      *template.Template
	settingsPageTmpl        *template.Template
	repositoryPageTmpl      *template.Template
	pkgDetailHeaderTmpl     *template.Template
	pkgConfigInput          *template.Template
	pkgUninstallModalTmpl   *template.Template
	toastTmpl               *template.Template
	datalistTmpl            *template.Template
	pkgDiscussionBadgeTmpl  *template.Template
	yamlModalTmpl           *template.Template
	repoClientset           repoclient.RepoClientset
}

var (
	templatesBaseDir = "internal/web"
	templatesDir     = "templates"
	componentsDir    = path.Join(templatesDir, "components")
	pagesDir         = path.Join(templatesDir, "pages")
)

func (t *templates) watchTemplates() error {
	watcher, err := fsnotify.NewWatcher()
	err = multierr.Combine(
		err,
		watcher.Add(path.Join(templatesBaseDir, componentsDir)),
		watcher.Add(path.Join(templatesBaseDir, templatesDir, "layout")),
		watcher.Add(path.Join(templatesBaseDir, pagesDir)),
	)
	if err == nil {
		go func() {
			for range watcher.Events {
				t.parseTemplates()
			}
		}()
	}
	return err
}

func (t *templates) parseTemplates() {
	t.templateFuncs = template.FuncMap{
		"ForClPkgOverviewBtn": pkg_overview_btn.ForClPkgOverviewBtn,
		"ForPkgDetailBtns":    pkg_detail_btns.ForPkgDetailBtns,
		"ForPkgUpdateAlert":   pkg_update_alert.ForPkgUpdateAlert,
		"PackageManifestUrl": func(pkg ctrlpkg.Package) string {
			if !pkg.IsNil() {
				url, err := t.repoClientset.ForPackage(pkg).
					GetPackageManifestURL(pkg.GetSpec().PackageInfo.Name, pkg.GetSpec().PackageInfo.Version)
				if err == nil {
					return url
				}
			}
			return ""
		},
		"ForToast":          toast.ForToast,
		"ForPkgConfigInput": pkg_config_input.ForPkgConfigInput,
		"ForDatalist":       datalist.ForDatalist,
		"IsUpgradable":      semver.IsUpgradable,
		"Markdown": func(source string) template.HTML {
			var buf bytes.Buffer

			converter := goldmark.New(
				goldmark.WithExtensions(
					extension.Linkify,
				),
				goldmark.WithParserOptions(
					parser.WithASTTransformers(
						util.Prioritized(&ASTTransformer{}, 1000),
					),
				),
			)

			if err := converter.Convert([]byte(source), &buf); err != nil {
				return template.HTML("<p>" + source + "</p>")
			}

			return template.HTML(buf.String())
		},
		"Reversed": func(param any) any {
			kind := reflect.TypeOf(param).Kind()
			switch kind {
			case reflect.Slice, reflect.Array:
				val := reflect.ValueOf(param)

				ln := val.Len()
				newVal := make([]interface{}, ln)
				for i := 0; i < ln; i++ {
					newVal[ln-i-1] = val.Index(i).Interface()
				}

				return newVal
			default:
				return param
			}
		},
		"UrlEscape": func(param string) string {
			return template.URLQueryEscaper(param)
		},
		"IsRepoStatusReady": func(repo v1alpha1.PackageRepository) bool {
			cond := meta.FindStatusCondition(repo.Status.Conditions, string(condition.Ready))
			return cond != nil && cond.Status == metav1.ConditionTrue
		},
		"PackageDetailRefreshId":          webutil.PackageRefreshDetailId,
		"PackageDetailHeaderRefreshId":    webutil.PackageRefreshDetailHeaderId,
		"PackageOverviewRefreshId":        webutil.PackageOverviewRefreshId,
		"ClusterPackageOverviewRefreshId": webutil.ClusterPackageOverviewRefreshId,
		"ComponentName":                   depUtil.ComponentName,
		"AutoUpdateEnabled": func(pkg ctrlpkg.Package) bool {
			if pkg != nil && !pkg.IsNil() {
				return pkg.AutoUpdatesEnabled()
			}
			return false
		},
		"IsSuspended": func(pkg ctrlpkg.Package) bool {
			if pkg != nil && !pkg.IsNil() {
				return pkg.GetSpec().Suspend
			}
			return false
		},
	}

	t.baseTemplate = template.Must(template.New("base.html").
		Funcs(t.templateFuncs).
		ParseFS(webFs, path.Join(templatesDir, "layout", "base.html")))
	t.clusterPkgsPageTemplate = t.pageTmpl("clusterpackages.html")
	t.pkgsPageTmpl = t.pageTmpl("packages.html")
	t.pkgPageTmpl = t.pageTmpl("package.html")
	t.pkgDiscussionPageTmpl = t.pageTmpl("discussion.html")
	t.supportPageTmpl = t.pageTmpl("support.html")
	t.bootstrapPageTmpl = t.pageTmpl("bootstrap.html")
	t.kubeconfigPageTmpl = t.pageTmpl("kubeconfig.html")
	t.settingsPageTmpl = t.pageTmpl("settings.html")
	t.repositoryPageTmpl = t.pageTmpl("repository.html")
	t.pkgDetailHeaderTmpl = t.componentTmpl("pkg-detail-header", "pkg-detail-btns")
	t.pkgConfigInput = t.componentTmpl("pkg-config-input", "datalist")
	t.pkgUninstallModalTmpl = t.componentTmpl("pkg-uninstall-modal")
	t.toastTmpl = t.componentTmpl("toast")
	t.datalistTmpl = t.componentTmpl("datalist")
	t.pkgDiscussionBadgeTmpl = t.componentTmpl("discussion-badge")
	t.yamlModalTmpl = t.componentTmpl("yaml-modal")
}

func (t *templates) pageTmpl(fileName string) *template.Template {
	return template.Must(
		template.Must(t.baseTemplate.Clone()).ParseFS(
			webFs,
			path.Join(pagesDir, fileName),
			path.Join(componentsDir, "*.html")))
}

func (t *templates) componentTmpl(id string, requiredTemplates ...string) *template.Template {
	tpls := make([]string, 0)
	for _, requiredTmpl := range requiredTemplates {
		tpls = append(tpls, path.Join(componentsDir, requiredTmpl+".html"))
	}
	tpls = append(tpls, path.Join(componentsDir, id+".html"))
	return template.Must(
		template.New(id).Funcs(t.templateFuncs).ParseFS(
			webFs,
			tpls...))
}

type ASTTransformer struct{}

func (g *ASTTransformer) Transform(node *ast.Document, reader text.Reader, pc parser.Context) {
	_ = ast.Walk(node, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		switch v := n.(type) {
		case *ast.Link:
			v.SetAttributeString("target", "_blank")
			v.SetAttributeString("rel", "noopener noreferrer")
		case *ast.Blockquote:
			v.SetAttributeString("class", "border-start border-primary border-3 ps-2")
		}

		return ast.WalkContinue, nil
	})
}
