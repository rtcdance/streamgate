# StreamGate Interview Rehearsal Flow

## Goal

Use this flow to rehearse the project in the same order you would present it in a real interview.

## Step 1: 30-90 second opening

Use:

- [`P1_CHINESE_ORAL_SCRIPT.md`](/Users/mingo/Applications/workspace/web3/project/streamgate/docs/P1_CHINESE_ORAL_SCRIPT.md)

Recommended:

- start with the 90-second version
- if interrupted early, fall back to the 30-second version

What you should say first:

- this is an NFT-gated streaming backend in Go
- Web3 is used as the authorization layer
- the project is really about protected media access

## Step 2: 2-3 minute project walkthrough

Use:

- [`P1_FINAL_INTERVIEW_SCRIPT.md`](/Users/mingo/Applications/workspace/web3/project/streamgate/docs/P1_FINAL_INTERVIEW_SCRIPT.md)
- [`P1_MEDIA_WEB3_INTERVIEW_GUIDE.md`](/Users/mingo/Applications/workspace/web3/project/streamgate/docs/P1_MEDIA_WEB3_INTERVIEW_GUIDE.md)

Recommended structure:

1. wallet challenge login
2. NFT verification
3. protected streaming
4. transcoding / worker path

## Step 3: Live demo

Use:

- [`P0_DEMO_CHECKLIST.md`](/Users/mingo/Applications/workspace/web3/project/streamgate/docs/P0_DEMO_CHECKLIST.md)

Recommended live order:

1. auth challenge
2. auth login
3. nft verify
4. repeated nft verify to show `cache_hit`
5. web3 rpc-status
6. protected manifest
7. protected segment
8. transcode submit
9. transcode status / tasks

## Step 4: Follow-up discussion

Prepare these likely topics:

1. why not verify NFT on every segment
2. why Web3 belongs in authorization instead of the whole system
3. why this project fits your media backend background
4. what is already implemented vs. what is still not production-complete
5. what you would improve next

Use:

- [`JOB_PRIORITY_RECOMMENDATION.md`](/Users/mingo/Applications/workspace/web3/project/streamgate/docs/JOB_PRIORITY_RECOMMENDATION.md)
- [`FINAL_STATE_SUMMARY.md`](/Users/mingo/Applications/workspace/web3/project/streamgate/docs/FINAL_STATE_SUMMARY.md)
- [`REMAINING_WORK_ITEMS.md`](/Users/mingo/Applications/workspace/web3/project/streamgate/docs/REMAINING_WORK_ITEMS.md)

## Step 5: Honest boundary statement

You should be ready to say clearly:

- no full DRM yet
- no fully production-grade RPC policy yet
- metadata is not fully complete end-to-end
- transcoding control plane is still a strong foundation, not a finished production platform

This improves credibility rather than hurting it.

## Step 6: Closing

Recommended closing line:

- I used a media backend scenario to show how my existing audio/video experience can transfer into Go backend work and Web3 authorization in a realistic product context.

## Best rehearsal routine

### Fast daily version

1. read the 90-second opening once
2. run through the demo checklist mentally
3. answer 2 follow-up questions out loud

### Full rehearsal version

1. 90-second opening
2. 3-minute walkthrough
3. live demo
4. 5 common follow-up questions
5. closing statement

## Final recommendation

Do not try to sound like you built everything.

Try to sound like:

- you built the important path
- you understand the trade-offs
- you know what is done
- you know what should come next

That is the most convincing version of the story.
