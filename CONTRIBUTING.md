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

If you decide to start working on a task, please help avoid confusion by setting its
status to 'In progress' and it will automatically be assigned to you.


## Providing pull requests

Pull requests are very welcome, and the maintainers of Mender work hard
to stay on top to review and hopefully merge your work.

If your work is significant, it can make sense to discuss the idea with the
maintainers and relevant project members upfront. Start a discussion via our
[Google group mailing
list](https://groups.google.com/a/lists.mender.io/forum/#!forum/mender).

Using commit signoffs and changelog tags is mandatory for all commits, see the
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

#### Examples:

* Given the commit message:

  ```
  Fix crash when /etc/mender/mender.conf is empty.
  ```

  This message is understandable by a user, and can therefore be used as is:

  ```
  Fix crash when /etc/mender/mender.conf is empty.

  Changelog: Title
  ```

* However, given the commit message:
  ```
  Implement mutex locking around user data.
  ```

  This is very developer centric and doesn't tell the user what changed for
  him. In this case it's appropriate to give a different changelog message, like
  this:

  ```
  Implement mutex locking around user data.

  Changelog: Fix crash when updating user data fields.
  ```

* In some cases it's appropriate not to provide a changelog message, for
  instance:

  ```
  Refactor dataProcess(), no functionality change.
  ```

  This is has no visible effect, therefore it's appropriate to add:

  ```
  Refactor dataProcess(), no functionality change.

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
