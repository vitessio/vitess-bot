# vitess-bot

This bot automates some tasks in the [`vitessio/vitess`](https://github.com/vitessio/vitess) git repo.

It currently automates the following tasks:
- Adds a review checklist comment on any Pull Request that is ready for review
- Creates backports against previous release branches for a Pull Request based on the original PR's backport labels
  - This will fail if there are any conflicts or other issues which require manual intervention

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
