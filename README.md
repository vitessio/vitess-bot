# vitess-bot

This bot automates some tasks in the [`vitessio/vitess`](https://github.com/vitessio/vitess) git repo.

It currently automates the following tasks:
- Adds a review checklist comment on any Pull Request that is ready for review.
- Adds the `NeedsWebsiteDocsUpdate`, `NeedsDescriptionUpdate`, and `NeedsIssue` labels to opened Pull Requests.
- Creates backports and forwardports
  - The suffix following the labels `Backport to: ` or `Forwardport to:` must match the [git branch name](https://github.com/vitessio/vitess/branches/all?query=release-)
  - If there is conflict, the backport PR will be created as a draft and a comment will be added to ping the author of the original PR.
- Automatic query serving error code documentation

## Installing the Bot
You can install and configure the bot with the following commands:
1. `git clone https://github.com/vitessio/vitess-bot.git`
2. `cd vitess-bot/`
3. `go build -o vitess-bot ./...`
4. `./vitess-bot`

An example of how we run the bot in production is available in `.github/workflows/deploy.yml`.

## Notes
:warning: When using [GitHub self-hosted runners](https://docs.github.com/en/actions/hosting-your-own-runners/about-self-hosted-runners), the bot should only be running on one of the runners at any given time.

## Local Testing

We first need to install `localtunnel`. This tool will give us a public URL that will be linked to our local environment. It allows GitHub to send us Webhooks.
Once installed, open a new terminal and run it: `lt --port 8080`. This will prompt you with the URL linked to your machine. In our GitHub App configuration we are going to use this URL.
The URL changes whenever we re-start the `lt` command, so you might need to update the configuration of your GitHub App after restarting `lt`.


In order to test the bot locally you will need to create a new GitHub App in https://github.com/settings/apps.
- You can name it however you want.
- The `Homepage URL` can be `https://vitess.io` or anything else.
- The `Identifying and authorizing users` and `Post installation` sections can be left empty.
- In the `Webhook` section you will need to fill in the `Webhook URL`. You can get this value by running `lt --port 8080` locally, this will print the URL linked to your local environment. Use that URL in the field. You must add `/api/github/hook` after the URL printed by `lt`, to redirect the webhooks to the correct API path (i.e. `https://lazy-frogs-hear.loca.lt/api/github/hook`).
- You also need to set a `Webhook secret` and save its value for later.
- In the section `Permissions`, we need for repository permissions: `Contents` (Read & Write), `Issues` (Read & Write), `Metadata` (Read Only), `Pull requests` (Read & Write)
- In the section `Subscribe to events` select: `Create`, `Issue comment`, `Issues`, `Pull request`, `Push`, and `Release`. Or any other permission depending on what you need for your local dev. 
- In the section `Where can this GitHub App be installed?`, select `Any account`.
- Click on `Create GitHub App`.

Once created, you can install your App. I recommend installing your app to your own fork of Vitess.

We now need to generate an SSH Key for our App. Go to the settings page of your App, scroll down and click `Generate a private key`. Download the key and put the file in the `.data/` directory of this repository.

Now, create an `.env` file at the root. The file is formatted as follows:

```dotenv
SERVER_ADDRESS=127.0.0.1
REVIEW_CHECKLIST_PATH=./config/review_checklist.txt
BOT_USER_LOGIN=vitess-bot[bot]
PRIVATE_KEY_PATH=.data/<NAME_OF_YOUR_SSH_PRIVATE_KEY_FILE>
GITHUB_APP_INTEGRATION_ID=<SIX_FIGURES_APP_ID>
GITHUB_APP_WEBHOOK_SECRET=<SECRETS_YOU_CREATED_EARLIER>
GITHUB_V3_API_URL=https://api.github.com/
```

Replace the placeholders with the proper values. You will be able to find `GITHUB_APP_INTEGRATION_ID` in the `General` page of your GitHub App under `App ID`.

Note that the `BOT_USER_LOGIN` is the name you gave the App you created above, _plus_ the literal `[bot]` on the end.

Once that is done, you should be able to run the program!