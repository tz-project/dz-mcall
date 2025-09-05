# Leader Election Implementation Guide

## Overview

This project has been enhanced with Kubernetes Leader Election functionality. This allows one pod among multiple pods to be elected as a leader to distribute tasks, while the remaining pods act as workers to process assigned tasks.

## Implemented Features

### 1. Leader Election
- Leader election using Kubernetes `coordination.k8s.io/v1/leases` resource
- Automatic new leader election when the current leader fails or terminates
- Leader creates and distributes tasks every 5 minutes

### 2. Task Distribution
- Leader pod assigns tasks to worker pods through ConfigMaps
- Each task is stored in ConfigMap with a unique ID
- Worker pods check and process assigned tasks every 30 seconds

### 3. Worker Processing
- Worker pods execute assigned tasks using existing `execCmd` logic
- Task completion status is recorded in ConfigMap after processing

## Setup Instructions

### 1. Add Dependencies
```bash
go mod tidy
go mod vendor
```

### 2. Environment Variables
The following environment variables are set in CronJob:
- `LEADER_ELECTION=true`: Enable leader election
- `NAMESPACE`: Namespace where pods are running
- `HOSTNAME`: Pod name (used as identifier for leader election)

### 3. RBAC Permissions
The `k8s/k8s-rbac.yaml` file includes the following permissions:
- `coordination.k8s.io/leases`: Permissions for leader election
- `pods`: Permissions to list worker pods
- `configmaps`: Permissions for task assignment

## Usage

### 1. Kubernetes Deployment
```bash
# Apply RBAC permissions
kubectl apply -f k8s/k8s-rbac.yaml

# Deploy CronJob
kubectl apply -f k8s/k8s-crontab.yaml
```

### 2. Log Monitoring
```bash
# Check leader pod logs
kubectl logs -l app=dz-mcall-${GIT_BRANCH} -f

# Check task processing status for specific pod
kubectl get configmaps -l app=dz-mcall,task=true
```

## Operation Flow

1. **Startup**: Each pod participates in leader election when started
2. **Leader Election**: One pod is elected as leader
3. **Task Generation**: Leader creates task list every 5 minutes
4. **Task Distribution**: Leader assigns tasks to worker pods via ConfigMap
5. **Task Processing**: Worker pods process assigned tasks and log results
6. **Status Update**: Task completion status is recorded in ConfigMap after processing

## Configuration File Example

### mcall.yaml Configuration
```yaml
request:
  input: |
    {
      "inputs": [
        {
          "input": "echo 'Hello from task 1'",
          "type": "cmd",
          "name": "task1"
        },
        {
          "input": "echo 'Hello from task 2'",
          "type": "cmd", 
          "name": "task2"
        }
      ]
    }
```

## Monitoring

### Task Status Check
```bash
# Check completed tasks
kubectl get configmaps -l app=dz-mcall,task=true -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.metadata.annotations.processed}{"\t"}{.metadata.annotations.processed-at}{"\n"}{end}'

# Check tasks assigned to specific pod
kubectl get configmaps -l app=dz-mcall,task=true,assigned-to=<pod-name>
```

### Leader Status Check
```bash
# Check current leader
kubectl get lease dz-mcall-leader -o jsonpath='{.spec.holderIdentity}'
```

## Considerations

1. **Resource Usage**: Consider cluster resources as multiple pods run simultaneously
2. **Network Policy**: Network policies may be required for pod-to-pod communication
3. **Log Management**: Manage logs separately for each pod
4. **Task Duplication**: Ensure the same task is not assigned to multiple pods

## Troubleshooting

### Common Issues

1. **Leader Election Failure**
   - Check RBAC permissions
   - Verify namespace configuration

2. **Task Assignment Failure**
   - Check ConfigMap creation permissions
   - Verify pod labels

3. **Task Processing Failure**
   - Check worker pod status
   - Review error messages in logs