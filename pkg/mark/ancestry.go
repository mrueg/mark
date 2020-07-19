package mark

import (
	"fmt"
	"strings"

	"github.com/kovetskiy/mark/pkg/confluence"
	"github.com/kovetskiy/mark/pkg/log"
	"github.com/reconquest/karma-go"
)

func EnsureAncestry(
	dryRun bool,
	api *confluence.API,
	space string,
	ancestry []string,
) (*confluence.PageInfo, error) {
	var parent *confluence.PageInfo

	rest := ancestry

	for i, title := range ancestry {
		page, err := api.FindPage(space, title)
		if err != nil {
			return nil, karma.Format(
				err,
				`error during finding parent page with title %q`,
				title,
			)
		}

		if page == nil {
			break
		}

		log.Debugf(nil, "parent page %q exists: %s", title, page.Links.Full)

		rest = ancestry[i:]
		parent = page
	}

	if parent != nil {
		rest = rest[1:]
	} else {
		page, err := api.FindRootPage(space)
		if err != nil {
			return nil, karma.Format(
				err,
				"can't find root page for space %q",
				space,
			)
		}

		parent = page
	}
	if len(rest) == 0 {
		return parent, nil
	}

	log.Debugf(
		nil,
		"empty pages under %q to be created: %s",
		parent.Title,
		strings.Join(rest, ` > `),
	)

	if !dryRun {
		for _, title := range rest {
			page, err := api.CreatePage(space, parent, title, ``)
			if err != nil {
				return nil, karma.Format(
					err,
					`error during creating parent page with title %q`,
					title,
				)
			}

			parent = page
		}
	} else {
		log.Infof(
			nil,
			"skipping page creation due to enabled dry-run mode, "+
				"need to create %d pages: %v",
			len(rest),
			rest,
		)
	}

	return parent, nil
}

func ValidateAncestry(
	api *confluence.API,
	space string,
	ancestry []string,
) (*confluence.PageInfo, error) {
	page, err := api.FindPage(space, ancestry[len(ancestry)-1])
	if err != nil {
		return nil, err
	}

	if page == nil {
		return nil, nil
	}

	if len(page.Ancestors) < 1 {
		return nil, fmt.Errorf(`page %q has no parents`, page.Title)
	}

	if len(page.Ancestors) < len(ancestry) {
		return nil, fmt.Errorf(
			"page %q has fewer parents than specified: %s",
			page.Title,
			strings.Join(ancestry, ` > `),
		)
	}

	for _, parent := range ancestry[:len(ancestry)-1] {
		found := false

		// skipping root article title
		for _, ancestor := range page.Ancestors[1:] {
			if ancestor.Title == parent {
				found = true
				break
			}
		}

		if !found {
			list := []string{}

			for _, ancestor := range page.Ancestors[1:] {
				list = append(list, ancestor.Title)
			}

			return nil, karma.Describe("expected parent", parent).
				Describe("list", strings.Join(list, "; ")).
				Format(
					nil,
					"unexpected ancestry tree, did not find expected parent page in the tree",
				)
		}
	}

	return page, nil
}