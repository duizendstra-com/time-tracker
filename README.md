# Time Tracker

Log project time in Google Calendar from its side panel. Time Tracker is a Google
Workspace add-on over HTTP, written in Go and run on Cloud Run. Workspace POSTs each
event as JSON, and the service answers with the card to draw.

- **Entries** are events in a calendar named `Time Tracker`, tagged with a client,
  project and task. They use the same format as the Apps Script version (see
  [AGENTS.md § The entry contract](AGENTS.md#the-entry-contract)).
- **Lists**: the client, project and task dropdowns come from your entries' tags. A
  "+ New …" choice adds a name, which stays on the list once an entry uses it.
- **Gemini** turns a typed note ("two hours on the Acme header") into a filled-in form.
  You check it and choose Create entry; nothing is written until you do.
- **Billable**: a checkbox on the form, kept as a fourth tag.
- **Open an entry** in Calendar to update or delete it.
- **Sync to sheet** copies every entry into a spreadsheet named `Time Tracker` in your
  Drive, which it makes the first time. It sees only the files it made (`drive.file`).
- **Rename…** changes a client, project or task in every entry that uses it, after a
  card that says how many entries change.

## Run it on your laptop

`read -rs` takes the key without showing it. Without a key the service still runs, but
the form has no Describe it section.

```bash
read -rs GEMINI_API_KEY && export GEMINI_API_KEY
go run .
```

In a second terminal, send it a test event:

```bash
curl -X POST http://localhost:8080/ \
  -H "Content-Type: application/json" \
  -d '{"commonEventObject": {"hostApp": "CALENDAR"}}'
```

It answers with the homepage card as JSON. A GET gets 405 Method Not Allowed:
Workspace only ever POSTs. A local run has no user token, so the lists are empty and
Create says to open Time Tracker from Calendar.

## Deploy it

These steps need a Cloud project and a Gemini key.

1. Prepare the project: the APIs, and a registry for `ko` to push to.

   ```bash
   export PROJECT_ID=your-project-id
   export REGION=europe-west1
   gcloud config set project "$PROJECT_ID"

   gcloud services enable run.googleapis.com artifactregistry.googleapis.com \
     secretmanager.googleapis.com gsuiteaddons.googleapis.com calendar-json.googleapis.com \
     sheets.googleapis.com drive.googleapis.com
   gcloud artifacts repositories create services \
     --repository-format=docker --location="$REGION"
   gcloud auth configure-docker "$REGION-docker.pkg.dev"
   export KO_DOCKER_REPO="$REGION-docker.pkg.dev/$PROJECT_ID/services"
   ```

2. Put the key in Secret Manager, and give the service its own identity, which can
   read that secret and nothing else.

   ```bash
   read -rs GEMINI_API_KEY
   printf %s "$GEMINI_API_KEY" | gcloud secrets create gemini-api-key --data-file=-
   unset GEMINI_API_KEY

   gcloud iam service-accounts create time-tracker
   export RUNTIME_SA="time-tracker@$PROJECT_ID.iam.gserviceaccount.com"
   gcloud secrets add-iam-policy-binding gemini-api-key \
     --member="serviceAccount:$RUNTIME_SA" \
     --role=roles/secretmanager.secretAccessor
   ```

3. Build it with `ko`, with no Dockerfile, and deploy it to Cloud Run. The service is
   private.

   ```bash
   gcloud run deploy time-tracker \
     --image="$(ko build .)" \
     --region="$REGION" \
     --service-account="$RUNTIME_SA" \
     --set-secrets=GEMINI_API_KEY=gemini-api-key:latest \
     --no-allow-unauthenticated
   ```

4. Let the add-on, and only the add-on, call the service.

   ```bash
   export ADDON_SA=$(gcloud workspace-add-ons get-authorization \
     --format='value(serviceAccountEmail)')
   gcloud run services add-iam-policy-binding time-tracker \
     --region="$REGION" \
     --member="serviceAccount:$ADDON_SA" \
     --role=roles/run.invoker
   ```

5. Register the add-on and install it for yourself. Both triggers call the service's one
   URL. `currentEventAccess: READ` and `calendar.addons.current.event.read` make
   Calendar send the opened event's id; `calendar` lets the service find, make and write
   the `Time Tracker` calendar with your token; `drive.file` lets Sync make and fill its
   one spreadsheet. Consent is granular: someone can leave a box unticked, and the
   add-on asks again for what an action needs when they use it.

   ```bash
   export SERVICE_URL=$(gcloud run services describe time-tracker \
     --region="$REGION" --format='value(status.url)')

   cat > deployment.local.json <<EOF
   {
     "oauthScopes": [
       "https://www.googleapis.com/auth/calendar.addons.execute",
       "https://www.googleapis.com/auth/calendar.addons.current.event.read",
       "https://www.googleapis.com/auth/calendar",
       "https://www.googleapis.com/auth/drive.file"
     ],
     "addOns": {
       "common": {
         "name": "Time Tracker (Go)",
         "logoUrl": "https://www.gstatic.com/images/icons/material/system/1x/schedule_black_24dp.png",
         "homepageTrigger": { "runFunction": "$SERVICE_URL" }
       },
       "calendar": {
         "currentEventAccess": "READ",
         "eventOpenTrigger": { "runFunction": "$SERVICE_URL" }
       }
     }
   }
   EOF

   gcloud workspace-add-ons deployments create time-tracker \
     --deployment-file=deployment.local.json
   gcloud workspace-add-ons deployments install time-tracker
   ```

   After a change to the deployment file, use `deployments replace` in place of
   `create`. After a code change, run step 3 again; nothing else changes.

6. Open https://calendar.google.com: Time Tracker (Go) is in the side panel.
