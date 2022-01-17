Contributing to Mender
======================

Thank you for showing interest in contributing to the Mender project.
Connecting with contributors and growing a community is very important to us.
We hope you will find what you need to get started on this page.

## Reporting security issues

If you come across any security issue, please bring it to our team's
attention as quickly as possible by sending an email to
[security@mender.io](mailto:security@mender.io).

Please do not disclose anything in public. Once an issue has been addressed we
will publish the fix and acknowledge your finding on our site if you so wish.


## Proposed tasks to get started

There is a `helpwanted` tag on some tasks in the Mender issue tracker
that have been identified as good candidates for initial contributors.

You can see them in the [Help Wanted saved filter](https://tracker.mender.io/issues/?filter=11500).


## Providing pull requests

Pull requests are very welcome, and the maintainers of Mender work hard
to stay on top to review and hopefully merge your work.

If your work is significant, it can make sense to discuss the idea with the
maintainers and relevant project members upfront. Start a discussion on our [Mender Hub forum](https://hub.mender.io/c/general-discussions).

Using commit signoffs and changelog tags is mandatory for all commits; we also
enforce the [conventional
commit](https://www.conventionalcommits.org/en/v1.0.0/) specification. See the
next sections for details.


### Sign your work

Mender is licensed under the Apache License, Version 2.0. To ensure open source
license compatibility, we need to keep track of the origin of all commits and
make sure they comply with the license. To do this, we follow the same procedure
as used by the Linux kernel, and ask every commit to be signed off.

The sign-off is a simple line at the end of the explanation for the patch, which
certifies that you wrote it or otherwise have the right to pass it on as an
open-source commit.  The rules are pretty simple: if you can certify the below
(from [developercertificate.org](http://developercertificate.org/)):


```
Developer Certificate of Origin
Version 1.1

Copyright (C) 2004, 2006 The Linux Foundation and its contributors.
660 York Street, Suite 102,
San Francisco, CA 94110 USA

Everyone is permitted to copy and distribute verbatim copies of this
license document, but changing it is not allowed.

Developer's Certificate of Origin 1.1

By making a contribution to this project, I certify that:

(a) The contribution was created in whole or in part by me and I
    have the right to submit it under the open source license
    indicated in the file; or

(b) The contribution is based upon previous work that, to the best
    of my knowledge, is covered under an appropriate open source
    license and I have the right under that license to submit that
    work with modifications, whether created in whole or in part
    by me, under the same open source license (unless I am
    permitted to submit under a different license), as indicated
    in the file; or

(c) The contribution was provided directly to me by some other
    person who certified (a), (b) or (c) and I have not modified
    it.

(d) I understand and agree that this project and the contribution
    are public and that a record of the contribution (including all
    personal information I submit with it, including my sign-off) is
    maintained indefinitely and may be redistributed consistent with
    this project or the open source license(s) involved.
```

Then you just add a line to every git commit message:

    Signed-off-by: Random J Developer <random@developer.example.org>

Use your real name (sorry, no pseudonyms or anonymous contributions).

If you set your `user.name` and `user.email` git configs, you can sign your
commit automatically with `git commit -s`.


### Changelog tags

Every commit requires a changelog tag to document what has changed from one
release to the next. Unlike commit messages, these should be written in a user
centric way.

#### Changelog tag types

Below is the complete list of possible tags. See also examples in the next
section.

* `Changelog: <message>` - Use `<message>` as the changelog entry. Message can
  span multiple lines, but is terminated by two consecutive newlines.

* `Changelog: Title` - Use the commit title (the first line) as the changelog
  entry.

* `Changelog: Commit` - Use the entire commit message as a changelog entry (but
  see filtered content below).

* `Changelog: None` - Don't generate a changelog entry for this commit.

A few things are always filtered from changelog entries: `cherry picked from...`
lines and `Signed-off-by:`, which are standard Git strings. In addition, any
reverted commit will automatically remove the corresponding entry from the
changelog output.

One commit can have several changelog tags, which will generate several entries,
if desired.

### Conventional commits

In Mender we have decided to use the conventional commit format to help
structure our commits.

This helps us automatically determine the semantic version bump for each of our
versioned repositories, based on the types of commits landed.

Conventional commits are a way to extract semantic meaning from a git log, and
is best introduced by reading the introduction on the site [conventional
commits](https://www.conventionalcommits.org/en/v1.0.0/).

#### In short:

The Conventional Commits specification is a lightweight convention on top of
commit messages. It provides an easy set of rules for creating an explicit
commit history; which makes it easier to write automated tools on top of. This
convention dovetails with SemVer, by describing the features, fixes, and
breaking changes made in commit messages.

In short, commits in Mender have to follow the schema:

```
<type>[optional scope]: <description>

[optional body]

[optional footer(s)]
```

The commit contains the following structural elements, to communicate intent to
the consumers of your library:

1. fix: a commit of the type fix patches a bug in your codebase (this correlates with PATCH in Semantic Versioning).
2. feat: a commit of the type feat introduces a new feature to the codebase (this correlates with MINOR in Semantic Versioning).
3. BREAKING CHANGE: a commit that has a footer BREAKING CHANGE:, or appends a ! after the type/scope, introduces a breaking API change (correlating with MAJOR in Semantic Versioning). A BREAKING CHANGE can be part of commits of any type.
4. types other than fix: and feat: are allowed, for example chore:, ci:, docs:, style:, refactor:, perf:, test:.

A scope may be provided to a commit’s type, to provide additional contextual
information and is contained within parenthesis, e.g., feat(parser): add ability
to parse arrays.

##### Examples:

```
feat: Add new command-line flag for resending the inventory

Changelog: A new flag is added which triggers a resending of the client inventory.
Signed-off-by: Random J Developer <random@developer.example.org>
```

A commit following <feat>: <Add new command-line flag for resending the
inventory>, and which also includes the required `Changelog`, and `signoff`.


A commit can also include a body, describing the changes in more depth.

```
feat: Add new command-line flag for resending the inventory

Aliquam erat volutpat.  Nunc eleifend leo vitae magna.  In id erat non orci commodo lobortis.  Proin neque massa, cursus ut, gravida ut, lobortis eget, lacus.  Sed diam.  Praesent fermentum tempor tellus.  Nullam tempus.  Mauris ac felis vel velit tristique imperdiet.  Donec at pede.  Etiam vel neque nec dui dignissim bibendum.  Vivamus id enim.  Phasellus neque orci, porta a, aliquet quis, semper a, massa.  Phasellus purus.  Pellentesque tristique imperdiet tortor.  Nam euismod tellus id erat.

Changelog: A new flag is added which triggers a resending of the client inventory.
Signed-off-by: Random J Developer <random@developer.example.org>
```

And, in the case of a breaking change, a `BREAKING CHANGE` footer is required.


```
refactor: Remove command-line flag for resending the inventory

BREAKING CHANGE: Removed the command-line flag for resending the inventory.

Changelog: A new flag is added which triggers a resending of the client inventory.
Signed-off-by: Random J Developer <random@developer.example.org>
```

#### Examples:

* Given the commit message:

  ```
  fix: crash when /etc/mender/mender.conf is empty.
  ```

  This message is understandable by a user, and can therefore be used as is:

  ```
  fix: crash when /etc/mender/mender.conf is empty.

  Changelog: Title
  ```

* However, given the commit message:
  ```
  fix: Implement mutex locking around user data.
  ```

  This is very developer centric and doesn't tell the user what changed for
  him. In this case it's appropriate to give a different changelog message, like
  this:

  ```
  fix: Implement mutex locking around user data.

  Changelog: Fix crash when updating user data fields.
  ```

* In some cases it's appropriate not to provide a changelog message, for
  instance:

  ```
  refactor: dataProcess(), no functionality change.
  ```

  This is has no visible effect, therefore it's appropriate to add:

  ```
  refactor: dataProcess(), no functionality change.

  Changelog: None
  ```

### Structuring your commits

More often than not, your pull request will come as a set of commits, not just
a single one. This is especially recommended in case of larger changesets.

In that case, please make sure that each commit constitutes a cohesive, logical
whole, e.g.  modifies a given package, function, or application layer. There are many ways
to conceptually divide your changeset, depending on its size and content - this is up to you.
It's just important that unrelated changes are not mixed up together in unrelated commits.

This is to ensure that:
* your PR is easy to browse and review
* git log is easier to digest

#### Example

* Bad:

```
refactor: X.

Changelog: None
```

```
refactor: Y to use X.
...but also, fix some more of X!

Changelog: None
```

```
chore: Add tests for X and Y.

Changelog: None
```

```
fix: even more of X and Y to make the tests pass, d'oh!

Changelog: None
```

These commits reflect an ordinary workflow of incremental
changes and fixes, but they are unwieldy for review because of
(un)related changes being scattered all over.

Use rebase with edits and squashes to rework this into something more cohesive:

* Good:
```
refactor: and test X.

Changelog: None
```

```
refactor: Y to use X, test Y.

Changelog: None
```


...or even:
```
refactor: X.

Changelog: None
```

```
chore: Add tests for X.

Changelog: None
```

```
refactor: Y to use X.

Changelog: None
```

```
chore: Add tests for Y.

Changelog: None
```

## Contributor Code of Conduct

We have a [Code of Conduct](https://github.com/mendersoftware/mender/blob/master/code-of-conduct.md) that applies to all contributors and participants to the Mender project.


## Let us work together with you

In an ever more digitized world, securing the world's connected devices is a
very important and meaningful task. To succeed, we will need to row in the same
direction and work to the best interest of the project.

This project appreciates your friendliness, transparency and a collaborative
spirit.
