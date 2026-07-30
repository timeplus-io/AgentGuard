package handlers

import (
	"errors"
	"fmt"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentguard/internal/auth"
	"github.com/timeplus/agentguard/internal/catalog"
	"github.com/timeplus/agentguard/internal/config"
	"github.com/timeplus/agentguard/internal/rules"
)

// packageEnablePlan splits a package's member rule IDs into those to install,
// those already installed (skipped), and those referencing a rule missing from
// the catalog. It is pure so it can be unit-tested without a live Timeplus.
type packageEnablePlan struct {
	ToInstall []string
	Skipped   []string
	Missing   []string
}

// planPackageEnable computes the enable plan for a package. Order of members is
// preserved. installed is the set of currently-installed rule IDs; exists
// reports whether a rule ID resolves to a catalog definition.
func planPackageEnable(members []string, installed map[string]bool, exists func(string) bool) packageEnablePlan {
	plan := packageEnablePlan{ToInstall: []string{}, Skipped: []string{}, Missing: []string{}}
	for _, id := range members {
		switch {
		case installed[id]:
			plan.Skipped = append(plan.Skipped, id)
		case !exists(id):
			plan.Missing = append(plan.Missing, id)
		default:
			plan.ToInstall = append(plan.ToInstall, id)
		}
	}
	return plan
}

// packageStatusCounts returns how many of a package's member rules exist in
// total, how many are installed, and how many of those are enabled. Installed
// entries unrelated to the package are ignored. Pure, for unit testing.
func packageStatusCounts(pkg catalog.Package, installed map[string]rules.RuleInstance) (total, installedCount, enabledCount int) {
	total = len(pkg.Rules)
	for _, id := range pkg.Rules {
		inst, ok := installed[id]
		if !ok {
			continue
		}
		installedCount++
		if inst.Enabled {
			enabledCount++
		}
	}
	return total, installedCount, enabledCount
}

// packageView is the read model for a rule package on GET /api/rules/packages.
// Name/Description are already localized; Rules is the member ID list.
type packageView struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	DefaultEnabled bool     `json:"default_enabled"`
	Rules          []string `json:"rules"`
	Total          int      `json:"total"`
	InstalledCount int      `json:"installed_count"`
	EnabledCount   int      `json:"enabled_count"`
}

// ListPackages returns all rule packages with per-package install/enable counts
// computed against the caller's installed rules. Name/Description are localized
// via ?locale=. Read scope: rbac ReadKey("rules").
func ListPackages(cfg *config.Config, mgr *catalog.Manager, lc *rules.Lifecycle) gin.HandlerFunc {
	return func(c *gin.Context) {
		reqCfg := auth.RequestCfg(c, cfg)
		installed, err := lc.For(&reqCfg.Timeplus).List()
		if err != nil {
			log.Printf("packages: ListPackages list: %v", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": fmt.Sprintf("rules query failed: %v", err)})
			return
		}
		installedMap := make(map[string]rules.RuleInstance, len(installed))
		for _, inst := range installed {
			installedMap[inst.RuleID] = inst
		}

		locale := resolveLocale(c)
		pkgs := mgr.Packages()
		views := make([]packageView, 0, len(pkgs))
		for _, p := range pkgs {
			lp := p.Localized(locale)
			total, installedCount, enabledCount := packageStatusCounts(p, installedMap)
			views = append(views, packageView{
				ID:             lp.ID,
				Name:           lp.Name,
				Description:    lp.Description,
				DefaultEnabled: lp.DefaultEnabled,
				Rules:          lp.Rules,
				Total:          total,
				InstalledCount: installedCount,
				EnabledCount:   enabledCount,
			})
		}
		c.JSON(http.StatusOK, gin.H{"packages": views})
	}
}

// packageEnableResult is the outcome of POST /api/rules/packages/:id/enable.
type packageEnableResult struct {
	Package   string              `json:"package"`
	Installed []string            `json:"installed"`
	Skipped   []string            `json:"skipped"`
	Missing   []string            `json:"missing"`
	Failed    []packageEnableFail `json:"failed"`
}

type packageEnableFail struct {
	RuleID string `json:"rule_id"`
	Error  string `json:"error"`
}

// EnablePackage bulk-installs a package's member rules through the lifecycle
// service, each with default parameters. Rules already installed are skipped;
// rules missing from the catalog are reported; per-rule install failures are
// collected without aborting the batch. Write scope: rbac WriteKey("rules").
func EnablePackage(cfg *config.Config, mgr *catalog.Manager, lc *rules.Lifecycle) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		pkg, ok := mgr.PackageByID(id)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("package %q not found", id)})
			return
		}

		reqCfg := auth.RequestCfg(c, cfg)
		svc := lc.For(&reqCfg.Timeplus)
		installed, err := svc.List()
		if err != nil {
			log.Printf("packages: EnablePackage %s list: %v", id, err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": fmt.Sprintf("rules query failed: %v", err)})
			return
		}
		installedSet := make(map[string]bool, len(installed))
		for _, inst := range installed {
			installedSet[inst.RuleID] = true
		}

		plan := planPackageEnable(pkg.Rules, installedSet, mgr.Exists)
		actor := sessionUser(c)
		reason := fmt.Sprintf("enable package %s", id)

		result := packageEnableResult{
			Package:   id,
			Installed: []string{},
			Skipped:   plan.Skipped,
			Missing:   plan.Missing,
			Failed:    []packageEnableFail{},
		}
		for _, ruleID := range plan.ToInstall {
			if _, err := svc.Install(ruleID, nil, actor, reason); err != nil {
				// A concurrent install may have landed first; treat as skipped.
				if errors.Is(err, rules.ErrAlreadyInstalled) {
					result.Skipped = append(result.Skipped, ruleID)
					continue
				}
				log.Printf("packages: EnablePackage %s install %s: %v", id, ruleID, err)
				result.Failed = append(result.Failed, packageEnableFail{RuleID: ruleID, Error: err.Error()})
				continue
			}
			result.Installed = append(result.Installed, ruleID)
		}

		c.JSON(http.StatusOK, result)
	}
}
