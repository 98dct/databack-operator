# databack-operator
基于crd-operator的数据备份

搭建步骤：
1.下载kubebuilder3.6 注意和k8s版本 go版本对应 
    执行make build初始化kubebuilder， 把kubebuilder放在/usr/local/bin下面， kubebuilder version查看版本
    
2.下载kustomize3.8.7 需要放在项目bin目录下

3.创建operator项目

4.执行kubebuilder init databack-operator --domain="operator.com.dct" --project-name="databack-operator" 
 --repo="databack-operator" 初始化项目
 
5.执行kubebuilder creat api --group "" --version v1 --kind Databack

6.发布crd   make install 注意kustomize要在bin/目录下

7.打包镜像   docker build -t 98dct/databack_operator:v1 .

8.上传镜像到仓库 docker push 98dct/databack_operator:v1

9.安装operator make deploy IMG=98dct/databack_operator:v1

10.部署在/config/samples下的示例cr yaml, 然后可以kubectl logs pods -n namespace 查看operator运行日志 
