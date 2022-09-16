# gitsearch
Better search for github.com (or a GitHub Enterprise store)

For some strange reason GitHub search doesn't seem to allow "precise match". This makes it very difficult to find
parts of code that you know should be there (i.e. paths or assignments and such).

`gitsearch` runs a regular search on github, but then filters the results for full matches of the input query.

For example if you search for `var a = 42` a regular github search will return matches for `var` or `a` or `42`,
but `gitsearch` will filter the result to only the lines that matches `var a = 42`.

Also, `gitsearch` will show snipped of codes around the results and order by repositories and files.

# Usage:

```
gitsearch [-all] [-browse] [-filter string] [-format text|html] [-highlight] query string

  or 

gitsearch -orgs

where:
  -all
    	print all results
  -browse
    	if format=html, open browser
  -debug
    	enable request tracing
  -filter string
    	apply these filters
  -format string
    	output format (text or html) (default "text")
  -highlight
    	highlight match in source code
  -orgs
    	list all organizations
  -server string
    	Enterprise server URL (use $GITHUB_BASE_URL if set, or https://api.github.com/ if empty)
  -token string
    	authentication token (use $GITHUB_TOKEN if set)
  -verbose
    	enable logging of results
```

# Authentication

If the environment variable `GITHUB_BASE_URL` is set or the `-server` parameter is passed,
it should point to a GitHub Enterprise server. Otherwise gitsearch will search `https://github.com/`.

If the environment variable `GITHUB_TOKEN` is set it will be used as the authentication token, otherwise the `-token`
parameter is expected to be set.

# Filters

The `-filter` parameter can be used to set additional filters that will not be matched on the result queries.
For example `user:{you}`, `language:go`, etc.
