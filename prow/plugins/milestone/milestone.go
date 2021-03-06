/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package setmilestone implements the `/milestone` command which allows members of the milestone
// maintainers team to specify a milestone to be applied to an Issue or PR.
package milestone

import (
	"bytes"
	"fmt"
	"regexp"

	"github.com/sirupsen/logrus"

	"k8s.io/test-infra/prow/github"
	"k8s.io/test-infra/prow/pluginhelp"
	"k8s.io/test-infra/prow/plugins"
)

const pluginName = "milestone"

var (
	milestoneRegex   = regexp.MustCompile(`(?m)^/milestone\s+(.+)$`)
	mustBeSigLead    = "You must be a member of the [%s](https://github.com/orgs/%s/teams/%s/members) github team to set the milestone."
	invalidMilestone = "The provided milestone is not valid for this repository.  Here are the available milestones:\n %s"
	clearKeyword     = "clear"
)

type githubClient interface {
	CreateComment(owner, repo string, number int, comment string) error
	ClearMilestone(org, repo string, num int) error
	SetMilestone(org, repo string, issueNum, milestoneNum int) error
	ListTeamMembers(id int) ([]github.TeamMember, error)
	ListMilestones(org, repo string) ([]github.Milestone, error)
}

func init() {
	plugins.RegisterGenericCommentHandler(pluginName, handleGenericComment, helpProvider)
}

func helpProvider(config *plugins.Configuration, enabledRepos []string) (*pluginhelp.PluginHelp, error) {
	pluginHelp := &pluginhelp.PluginHelp{
		Description: "The milestone plugin allows members of a configurable GitHub team to set the milestone on an issue or pull request.",
	}
	pluginHelp.AddCommand(pluginhelp.Command{
		Usage:       "/milestone <version> or /milestone clear",
		Description: "Updates the milestone for an issue or PR",
		Featured:    false,
		WhoCanUse:   "Members of the milestone maintainers GitHub team can use the '/milestone' command.",
		Examples:    []string{"/milestone v1.10", "/milestone v1.9", "/milestone clear"},
	})
	return pluginHelp, nil
}

func handleGenericComment(pc plugins.PluginClient, e github.GenericCommentEvent) error {
	return handle(pc.GitHubClient, pc.Logger, &e, pc.PluginConfig.Milestone.MaintainersID, pc.PluginConfig.Milestone.MaintainersTeam)
}

func buildMilestoneMap(milestones []github.Milestone) map[string]int {
	m := make(map[string]int)
	for _, ms := range milestones {
		m[ms.Title] = ms.Number
	}
	return m
}
func handle(gc githubClient, log *logrus.Entry, e *github.GenericCommentEvent, maintainersID int, maintainersName string) error {
	if e.Action != github.GenericCommentActionCreated {
		return nil
	}

	milestoneMatch := milestoneRegex.FindStringSubmatch(e.Body)
	if len(milestoneMatch) != 2 {
		return nil
	}

	org := e.Repo.Owner.Login
	repo := e.Repo.Name

	milestoneMaintainers, err := gc.ListTeamMembers(maintainersID)
	if err != nil {
		return err
	}
	found := false
	for _, person := range milestoneMaintainers {
		login := github.NormLogin(e.User.Login)
		if github.NormLogin(person.Login) == login {
			found = true
			break
		}
	}
	if !found {
		// not in the milestone maintainers team
		msg := fmt.Sprintf(mustBeSigLead, maintainersName, org, maintainersName)
		return gc.CreateComment(org, repo, e.Number, plugins.FormatResponseRaw(e.Body, e.HTMLURL, e.User.Login, msg))
	}

	milestones, err := gc.ListMilestones(org, repo)
	if err != nil {
		log.WithError(err).Errorf("Error listing the milestones in the %s/%s repo", org, repo)
		return err
	}
	proposedMilestone := milestoneMatch[1]

	// special case, if the clear keyword is used
	if proposedMilestone == clearKeyword {
		if err := gc.ClearMilestone(org, repo, e.Number); err != nil {
			log.WithError(err).Errorf("Error clearing the milestone for %s/%s#%d.", org, repo, e.Number)
		}
		return nil
	}

	milestoneMap := buildMilestoneMap(milestones)
	milestoneNumber, ok := milestoneMap[proposedMilestone]
	if !ok {
		var buffer bytes.Buffer
		buffer.WriteString("\t 1." + clearKeyword + "\n")
		for k := range milestoneMap {
			buffer.WriteString("\t 1." + k + "\n")
		}
		msg := fmt.Sprintf(invalidMilestone, buffer.String())
		return gc.CreateComment(org, repo, e.Number, plugins.FormatResponseRaw(e.Body, e.HTMLURL, e.User.Login, msg))
	}

	if err := gc.SetMilestone(org, repo, e.Number, milestoneNumber); err != nil {
		log.WithError(err).Errorf("Error adding the milestone %s to %s/%s#%d.", proposedMilestone, org, repo, e.Number)
	}

	return nil
}
