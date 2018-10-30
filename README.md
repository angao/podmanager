# PodManager

> **注意：** 当 `PodManager` 资源对象更新后，会将 `status.updated` 更新为 `true`。所以，每次修改 `PodManager` 后，如果想使之生效，还需要将 `status.updated` 更新为 `false`。

## 安装

通过以下命令将 CRD 安装进 Kubernetes Cluster

```bash
make install
```

## 部署

部署分为三部分：

- 将本项目打包成 image

```bash
make docker-build
```

- 安装项目运行需要的 Kubernetes 权限
- 将项目以 Deployment 的方式运行在 Kubernetes Cluster 中

```bash
make deploy
```

## 使用

参考 `config/samples` 目录下的 `yaml` 配置文件

- 指定 Pod 缩容
    - 绑定(注意 `strategy` 字段，此时 `type` 是 `PodDelete`，`phase` 是 `Binding`)
        ```yaml
        apiVersion: extensions.sncloud.com/v1alpha1
        kind: PodManager
        metadata:
          labels:
            controller-tools.k8s.io: "1.0"
          name: podscale
          namespace: default
        spec:
          deploymentName: site
          ipSet:
            - "172.17.0.15"
            - "172.17.0.16"
          strategy:
            type: PodDelete
            phase: Binding
        ```
    - 删除(更新 `strategy` 的 `phase` 字段为 `Deleting`)
        ```yaml
        apiVersion: extensions.sncloud.com/v1alpha1
        kind: PodManager
        metadata:
          labels:
            controller-tools.k8s.io: "1.0"
          name: podscale
          namespace: default
        spec:
          deploymentName: site
          ipSet:
            - "172.17.0.15"
            - "172.17.0.16"
          strategy:
            type: PodDelete
            phase: Deleting
        ```
- 应用灰度升级
    - 不指定灰度升级时间，即立即灰度升级(注意 `strategy` 字段值是 `PodUpgrade`)
        ```yaml
        apiVersion: extensions.sncloud.com/v1alpha1
        kind: PodManager
        metadata:
          labels:
            controller-tools.k8s.io: "1.0"
          name: grayscale
          namespace: default
        spec:
          deploymentName: site
          ipSet:
            - "172.17.0.15"
            - "172.17.0.16"
          strategy:
            type: PodUpgrade
          resources:
            containers:
            - containerName: app
              image: app:v2
              resources:
                limits:
                  cpu: 300m
                  memory: 200Mi
                requests:
                  cpu: 200m
                  memory: 100Mi
        ```
    - 指定灰度升级时间(指定的时间字段是 `scaleTimestamp`)
        ```yaml
        apiVersion: extensions.sncloud.com/v1alpha1
        kind: PodManager
        metadata:
          labels:
            controller-tools.k8s.io: "1.0"
          name: grayscale
          namespace: default
        spec:
          deploymentName: site
          scaleTimestamp: "2018-09-25 00:00:00"
          ipSet:
            - "172.17.0.15"
            - "172.17.0.16"
          strategy:
            type: PodUpgrade
          resources:
            containers:
            - containerName: app
              image: app:v2
              resources:
                limits:
                  cpu: 300m
                  memory: 200Mi
                requests:
                  cpu: 200m
                  memory: 100Mi
        ```
