package bindingruleupdate

import (
	"flag"
	"fmt"

	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/command/acl"
	"github.com/hashicorp/consul/command/flags"
	"github.com/mitchellh/cli"
)

func New(ui cli.Ui) *cmd {
	c := &cmd{UI: ui}
	c.init()
	return c
}

type cmd struct {
	UI    cli.Ui
	flags *flag.FlagSet
	http  *flags.HTTPFlags
	help  string

	ruleID string

	description  string
	selector     string
	roleBindType string
	roleName     string

	noMerge  bool
	showMeta bool
}

func (c *cmd) init() {
	c.flags = flag.NewFlagSet("", flag.ContinueOnError)
	c.flags.BoolVar(
		&c.showMeta,
		"meta",
		false,
		"Indicates that binding rule metadata such "+
			"as the content hash and raft indices should be shown for each entry.",
	)

	c.flags.StringVar(
		&c.ruleID,
		"id",
		"",
		"The ID of the binding rule to update. "+
			"It may be specified as a unique ID prefix but will error if the prefix "+
			"matches multiple binding rule IDs",
	)

	c.flags.StringVar(
		&c.description,
		"description",
		"",
		"A description of the binding rule.",
	)
	c.flags.StringVar(
		&c.selector,
		"selector",
		"",
		"Selector is an expression that matches against verified identity "+
			"attributes returned from the identity provider during login.",
	)
	c.flags.StringVar(
		&c.roleBindType,
		"role-bind-type",
		string(api.BindingRuleRoleBindTypeService),
		"Type of role binding to perform (\"service\" or \"existing\").",
	)
	c.flags.StringVar(
		&c.roleName,
		"role-name",
		"",
		"Name of role to bind on match. Can use {{var}} interpolation. "+
			"This flag is required.",
	)

	c.flags.BoolVar(
		&c.noMerge,
		"no-merge",
		false,
		"Do not merge the current binding rule "+
			"information with what is provided to the command. Instead overwrite all fields "+
			"with the exception of the binding rule ID which is immutable.",
	)

	c.http = &flags.HTTPFlags{}
	flags.Merge(c.flags, c.http.ClientFlags())
	flags.Merge(c.flags, c.http.ServerFlags())
	c.help = flags.Usage(help, c.flags)
}

func (c *cmd) Run(args []string) int {
	if err := c.flags.Parse(args); err != nil {
		return 1
	}

	if c.ruleID == "" {
		c.UI.Error(fmt.Sprintf("Cannot update a binding rule without specifying the -id parameter"))
		return 1
	}

	client, err := c.http.APIClient()
	if err != nil {
		c.UI.Error(fmt.Sprintf("Error connecting to Consul agent: %s", err))
		return 1
	}

	ruleID, err := acl.GetBindingRuleIDFromPartial(client, c.ruleID)
	if err != nil {
		c.UI.Error(fmt.Sprintf("Error determining binding rule ID: %v", err))
		return 1
	}

	// Read the current binding rule in both cases so we can fail better if not found.
	currentRule, _, err := client.ACL().BindingRuleRead(ruleID, nil)
	if err != nil {
		c.UI.Error(fmt.Sprintf("Error when retrieving current binding rule: %v", err))
		return 1
	} else if currentRule == nil {
		c.UI.Error(fmt.Sprintf("Binding rule not found with ID %q", ruleID))
		return 1
	}

	var rule *api.ACLBindingRule
	if c.noMerge {
		if c.roleName == "" {
			c.UI.Error(fmt.Sprintf("Missing required '-role-name' flag"))
			c.UI.Error(c.Help())
			return 1
		}

		rule = &api.ACLBindingRule{
			ID:           ruleID,
			IDPName:      currentRule.IDPName, // immutable
			Description:  c.description,
			RoleBindType: api.BindingRuleRoleBindType(c.roleBindType),
			RoleName:     c.roleName,
			Selector:     c.selector,
		}

	} else {
		rule = currentRule

		if c.description != "" {
			rule.Description = c.description
		}
		if c.roleName != "" {
			rule.RoleName = c.roleName
		}
		if isFlagSet(c.flags, "role-bind-type") {
			rule.RoleBindType = api.BindingRuleRoleBindType(c.roleBindType) // empty is valid
		}
		if isFlagSet(c.flags, "selector") {
			rule.Selector = c.selector // empty is valid
		}
	}

	rule, _, err = client.ACL().BindingRuleUpdate(rule, nil)
	if err != nil {
		c.UI.Error(fmt.Sprintf("Error updating binding rule %q: %v", ruleID, err))
		return 1
	}

	c.UI.Info(fmt.Sprintf("Binding rule updated successfully"))
	acl.PrintBindingRule(rule, c.UI, c.showMeta)
	return 0
}

func (c *cmd) Synopsis() string {
	return synopsis
}

func (c *cmd) Help() string {
	return flags.Usage(c.help, nil)
}

func isFlagSet(flags *flag.FlagSet, name string) bool {
	found := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}

const synopsis = "Update an ACL Binding Rule"
const help = `
Usage: consul acl binding-rule update -id ID [options]

  Updates a binding rule. By default it will merge the binding rule
  information with its current state so that you do not have to provide all
  parameters. This behavior can be disabled by passing -no-merge.

    Update all editable fields of the binding rule:

     $ consul acl binding-rule update \
            -id=43cb72df-9c6f-4315-ac8a-01a9d98155ef \
            -description="new description" \
            -role-bind-type=existing \
            -role-name="k8s-{{serviceaccount.name}}" \
            -selector='serviceaccount.namespace==default and serviceaccount.name==web'
`
