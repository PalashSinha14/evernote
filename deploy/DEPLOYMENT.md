# Deploying to AWS (ECS Fargate)

This is a step-by-step guide, not a script — deploying to a real AWS account
touches billing and shared infrastructure, so each step is meant to be run
and checked by a person, not automated blindly. Nothing in this repository
runs any of these commands for you.

Fargate rather than EC2: it removes the need to patch and size EC2 instances
yourself, which is not where this project's learning goals live. Everything
below still applies conceptually to an EC2-backed ECS cluster if you'd
rather manage the instances directly.

## Prerequisites

- An AWS account, and the [AWS CLI](https://aws.amazon.com/cli/) configured
  (`aws configure`) with credentials that can create ECR repositories, ECS
  resources, IAM roles and Secrets Manager entries.
- Docker, to build the image locally before pushing it.
- A reachable MongoDB. ECS runs the API; it does not run a database.
  [MongoDB Atlas](https://www.mongodb.com/atlas) (a free M0 cluster is
  enough) is the natural choice here, for the same reason it was suggested
  earlier in this project: nothing to patch, and it is reachable from
  outside your AWS account with a connection string, which a self-hosted
  Mongo on EC2 would need extra networking work to provide.

## 1. Push the image to ECR

```bash
export AWS_REGION=us-east-1          # pick your region
export ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text)

aws ecr create-repository --repository-name evernote-lite --region "$AWS_REGION"

aws ecr get-login-password --region "$AWS_REGION" \
  | docker login --username AWS --password-stdin "$ACCOUNT_ID.dkr.ecr.$AWS_REGION.amazonaws.com"

docker build -t evernote-lite .
docker tag evernote-lite:latest "$ACCOUNT_ID.dkr.ecr.$AWS_REGION.amazonaws.com/evernote-lite:latest"
docker push "$ACCOUNT_ID.dkr.ecr.$AWS_REGION.amazonaws.com/evernote-lite:latest"
```

## 2. Store the two secrets

`JWT_SECRET` and `MONGO_URI` are never set as plain environment values in the
task definition — anyone with read access to ECS could otherwise see them.
They come from Secrets Manager instead, referenced by ARN.

```bash
aws secretsmanager create-secret --name evernote-lite/jwt-secret \
  --secret-string "$(openssl rand -hex 32)"

aws secretsmanager create-secret --name evernote-lite/mongo-uri \
  --secret-string "mongodb+srv://<user>:<password>@<cluster>.mongodb.net/evernote_lite"
```

## 3. Create the IAM execution role

The task needs permission to pull the image from ECR, write logs, and read
the two secrets above. `AmazonECSTaskExecutionRolePolicy` covers ECR and
logs; the two secrets need an explicit policy naming them.

```bash
aws iam create-role --role-name ecsTaskExecutionRole \
  --assume-role-policy-document '{
    "Version": "2012-10-17",
    "Statement": [{
      "Effect": "Allow",
      "Principal": { "Service": "ecs-tasks.amazonaws.com" },
      "Action": "sts:AssumeRole"
    }]
  }'

aws iam attach-role-policy --role-name ecsTaskExecutionRole \
  --policy-arn arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy

aws iam put-role-policy --role-name ecsTaskExecutionRole \
  --policy-name evernote-lite-secrets \
  --policy-document '{
    "Version": "2012-10-17",
    "Statement": [{
      "Effect": "Allow",
      "Action": "secretsmanager:GetSecretValue",
      "Resource": [
        "arn:aws:secretsmanager:'"$AWS_REGION"':'"$ACCOUNT_ID"':secret:evernote-lite/jwt-secret*",
        "arn:aws:secretsmanager:'"$AWS_REGION"':'"$ACCOUNT_ID"':secret:evernote-lite/mongo-uri*"
      ]
    }]
  }'
```

## 4. Register the task definition

Fill in `<ACCOUNT_ID>`, `<REGION>` and `<YOUR_DOMAIN_OR_ALB_DNS_NAME>` in
[ecs-task-definition.json](ecs-task-definition.json), then:

```bash
aws logs create-log-group --log-group-name /ecs/evernote-lite --region "$AWS_REGION"
aws ecs register-task-definition --cli-input-json file://deploy/ecs-task-definition.json
```

## 5. Create the cluster, load balancer and service

```bash
aws ecs create-cluster --cluster-name evernote-lite
```

Put an Application Load Balancer in front of the service — a Fargate task's
own IP is not stable across deployments, and only the ALB gives you a fixed
DNS name (the value `APP_BASE_URL` should point at) plus a place to
terminate TLS. Create the ALB, a target group pointing at port 8080 with
`/healthz` as its health check path, and a listener, through the console or
`aws elbv2 create-load-balancer` / `create-target-group` / `create-listener`
— the exact commands depend on which VPC and subnets you're using, which is
why they are not templated here.

Then create the service, referencing that target group:

```bash
aws ecs create-service \
  --cluster evernote-lite \
  --service-name evernote-lite \
  --task-definition evernote-lite \
  --desired-count 1 \
  --launch-type FARGATE \
  --network-configuration "awsvpcConfiguration={subnets=[<SUBNET_IDS>],securityGroups=[<SECURITY_GROUP_ID>],assignPublicIp=ENABLED}" \
  --load-balancers "targetGroupArn=<TARGET_GROUP_ARN>,containerName=evernote-lite,containerPort=8080"
```

## 6. After it's running — the one code change this deployment shape needs

Every request now arrives at the container from the ALB, not from the
original caller directly. `internal/app/app.go` currently calls
`router.SetTrustedProxies(nil)`, deliberately trusting no proxy — the right
default for local development, where trusting the wrong thing would let a
caller spoof their IP and dodge the rate limiter (see the Phase 5 section of
the explain document). Behind this ALB, every request's real address is now
the ALB's, so the rate limiter would treat every visitor as one caller until
this is updated to trust it specifically:

```go
router.SetTrustedProxies([]string{"<ALB security group's CIDR, or the VPC CIDR>"})
```

This is a deliberate follow-up, not an oversight — it only becomes correct to
make once an actual trusted proxy exists in front of the service, which is
exactly what this deployment step creates.

## Rolling out a new version

```bash
docker build -t evernote-lite .
docker tag evernote-lite:latest "$ACCOUNT_ID.dkr.ecr.$AWS_REGION.amazonaws.com/evernote-lite:latest"
docker push "$ACCOUNT_ID.dkr.ecr.$AWS_REGION.amazonaws.com/evernote-lite:latest"

aws ecs update-service --cluster evernote-lite --service evernote-lite --force-new-deployment
```

`scripts/init-indexes.sh` is worth running against the production database
before a rollout that changes query patterns significantly, so index
creation happens ahead of time rather than under live traffic on first boot
— see that script's own header comment for why this is optional rather than
required.
