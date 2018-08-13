#include <cstdio>
#include <cstring>
#include <iostream>
#include <io.h>
#include <ctype.h>
#include <cstdlib>
#include <unistd.h>


using namespace std;

FILE *fp1;
FILE *fp2;
bool dexit;

int tmp=4;

int runtime=0;
int dfilenum;
char test[200];
char judgeready[200];
char input[200];
char command[200];
char trash[200];
char blankspace[200]=" ";
char stdcom_1[200]="baidu.exe ls ";
char stdcom_2[200]=" --stream";
char stdcom_3[200]="baidu.exe d ";
char stdcom_4[200]="/";
char stdcom_5[200]=" --save";
char stdcom_6[200]=" od:/temp/";
char stdcom_7[200]="onedrivecmd put ";


int download_progress(char inputfilename[200],int taskid_input)
{
	memset(command,0,sizeof(command));
	strcpy(command,stdcom_3);
	strcat(command,input);
	strcat(command,stdcom_4);
	strcat(command,inputfilename);
	strcat(command,stdcom_2);
	strcat(command,stdcom_5);
	system(command);
	runtime=0;
	printf("the download task %d:\"%s\" has finished\n",taskid_input,inputfilename);
	return 0;
	
}

int upload_progress(char inputfilename[200],int taskid_input)
{
	printf("the upload task %d:\"%s\" start\n",taskid_input,inputfilename);
	memset(command,0,sizeof(command));
	strcpy(command,stdcom_7);
	strcat(command,inputfilename);
	strcat(command,stdcom_6);
	strcat(command,input);
	system(command);
	printf("the upload task %d:\"%s\" has finished\n",taskid_input,inputfilename);
	return 0;
}

struct dfiletype
{
	char name[100];
} dfilename[1000];

int main()
{
	printf("input d folder name:");
	scanf("%s",input);
	printf("input filenum:");
	scanf("%d",&dfilenum);
	strcpy(command,stdcom_1);
	strcat(command,input);
	fp1=popen(command, "r");
	for(int i=1; i<=dfilenum; i++)
	{
		fscanf(fp1,"%s",dfilename[i].name);
	}
	pclose(fp1);
	for(int i=1;i<=dfilenum;i++)
	{
		printf("the transfer task %d:\"%s\" start\n",i,dfilename[i].name);
		download_progress(dfilename[i].name,i);
		upload_progress(dfilename[i].name,i);
		remove(dfilename[i].name);
		printf("the transfer task %d:\"%s\" has finished\n",i,dfilename[i].name);
	}
	//printf("dfilenum = %d",dfilenum);

	/*for(int i=1; i<=dfilenum; i++)
	{
		printf("%s\n",dfilename[i].name);
	}*/
	printf("transfer task success!\n");
	return 0;
}
