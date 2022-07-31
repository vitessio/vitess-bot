# vitess-bot

This bot automates some tasks in the [`vitessio/vitess`](https://github.com/vitessio/vitess) git repo.

It currently automates the following tasks:
- [Adds a review checklist comment](https://github.com/vitessio/vitess-bot/blob/ae114aeffa7883916940bd9641b5e5602a1bae9b/index.js#L4-L26) on any Pull Request that is ready for review ([example comment](https://github.com/vitessio/vitess/pull/10847#issuecomment-1195644642))
- [Creates backports](https://github.com/vitessio/vitess-bot/blob/ae114aeffa7883916940bd9641b5e5602a1bae9b/index.js#L117-L160) against previous release branches for a Pull Request based on the original PR's [backport labels](https://github.com/vitessio/vitess/labels?q=backport)
  - The portion of the label following the `Backport to: ` prefix must match the [git branch name](https://github.com/vitessio/vitess/branches/all?query=release-)
  - This will fail if there are any conflicts or other issues which require manual intervention; a comment will be added to the original PR if this occurs ([example comment](https://github.com/vitessio/vitess/pull/10847#issuecomment-1200248322))
- [Creates forwardports](https://github.com/vitessio/vitess-bot/blob/ae114aeffa7883916940bd9641b5e5602a1bae9b/index.js#L117-L160) against later release branches for a Pull Request based on the original PR's [forwardport labels](https://github.com/vitessio/vitess/labels?q=forwardport)
  - The portion of the label following the `Forwardport to: ` prefix must match the [git branch name](https://github.com/vitessio/vitess/branches)
  - This will fail if there are any conflicts or other issues which require manual intervention; a comment will be added to the original PR if this occurs ([example comment](https://github.com/vitessio/vitess/pull/10847#issuecomment-1200248322))

## Installing the Bot
You can install and configure the bot with the following commands:
1. `git clone https://github.com/vitessio/vitess-bot.git`
2. `cd vitess-bot/`
3. `docker build -t vitess-bot .`

## Running the Bot
You can run the bot with the following command:
`docker run -d --name vitess-bot -p 3000:3000 vitess-bot`

## Notes
:warning: When using [GitHub self-hosted runners](https://docs.github.com/en/actions/hosting-your-own-runners/about-self-hosted-runners), the bot should only be running on one of the runners at any given time.
