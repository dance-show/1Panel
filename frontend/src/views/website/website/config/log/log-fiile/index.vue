<template>
    <div v-loading="loading">
        <div>
            <el-form-item :label="$t('website.enable')">
                <el-switch v-model="data.enable" @change="updateEnable"></el-switch>
            </el-form-item>
            <div style="margin-top: 10px">
                <el-checkbox border v-model="tailLog" style="float: left" @change="changeTail">
                    {{ $t('commons.button.watch') }}
                </el-checkbox>
                <el-button
                    style="margin-left: 20px"
                    @click="onDownload"
                    icon="Download"
                    :disabled="data.content === ''"
                >
                    {{ $t('file.download') }}
                </el-button>
                <el-button style="margin-left: 20px" @click="cleanLog" icon="Delete" :disabled="data.content === ''">
                    {{ $t('commons.button.clean') }}
                </el-button>
            </div>
        </div>
        <br />
        <codemirror
            re="logContainer"
            style="height: calc(100vh - 430px); width: 100%"
            :autofocus="true"
            :placeholder="$t('website.noLog')"
            :indent-with-tab="true"
            :tabSize="4"
            :lineWrapping="true"
            :matchBrackets="true"
            theme="cobalt"
            :styleActiveLine="true"
            :extensions="extensions"
            v-model="content"
            :disabled="true"
            @ready="handleReady"
        />
    </div>
</template>
<script lang="ts" setup>
import { Codemirror } from 'vue-codemirror';
import { javascript } from '@codemirror/lang-javascript';
import { oneDark } from '@codemirror/theme-one-dark';
import { computed, nextTick, onMounted, onUnmounted, reactive, ref, shallowRef } from 'vue';
import { OpWebsiteLog } from '@/api/modules/website';
import { dateFormatForName, downloadWithContent } from '@/utils/util';
import { useDeleteData } from '@/hooks/use-delete-data';

const extensions = [javascript(), oneDark];
const props = defineProps({
    logType: {
        type: String,
        default: '',
    },
    id: {
        type: Number,
        default: 0,
    },
});
const logType = computed(() => {
    return props.logType;
});
const id = computed(() => {
    return props.id;
});
const loading = ref(false);
const data = ref({
    enable: false,
    content: '',
});
const tailLog = ref(false);
let timer: NodeJS.Timer | null = null;

const view = shallowRef();
const editorContainer = ref<HTMLDivElement | null>(null);
const handleReady = (payload) => {
    view.value = payload.view;
    editorContainer.value = payload.container;
};
const content = ref('');
const end = ref(false);
const lastContent = ref('');

const readReq = reactive({
    id: id.value,
    operate: 'get',
    logType: logType.value,
    page: 0,
    pageSize: 500,
});

const getContent = () => {
    if (!end.value) {
        readReq.page += 1;
    }
    OpWebsiteLog(readReq).then((res) => {
        if (!end.value && res.data.end) {
            lastContent.value = content.value;
        }
        data.value = res.data;
        if (res.data.content != '') {
            if (end.value) {
                content.value = lastContent.value + '\n' + res.data.content;
            } else {
                if (content.value == '') {
                    content.value = res.data.content;
                } else {
                    content.value = content.value + '\n' + res.data.content;
                }
            }
        }
        end.value = res.data.end;
        nextTick(() => {
            const state = view.value.state;
            view.value.dispatch({
                selection: { anchor: state.doc.length, head: state.doc.length },
            });
            view.value.focus();
        });
    });
};

const changeTail = () => {
    if (tailLog.value) {
        timer = setInterval(() => {
            getContent();
        }, 1000 * 5);
    } else {
        onCloseLog();
    }
};

const updateEnable = () => {
    const operate = data.value.enable ? 'enable' : 'disable';
    const req = {
        id: id.value,
        operate: operate,
        logType: logType.value,
    };
    loading.value = true;
    OpWebsiteLog(req)
        .then(() => {
            getContent();
        })
        .finally(() => {
            loading.value = false;
        });
};

const cleanLog = async () => {
    const req = {
        id: id.value,
        operate: 'delete',
        logType: logType.value,
    };
    try {
        await useDeleteData(OpWebsiteLog, req, 'commons.msg.delete');
        getContent();
    } catch (error) {
    } finally {
    }
};

const onDownload = async () => {
    downloadWithContent(data.value.content, logType.value + '-' + dateFormatForName(new Date()) + '.log');
};

const onCloseLog = async () => {
    tailLog.value = false;
    clearInterval(Number(timer));
    timer = null;
};

function isScrolledToBottom(element: HTMLElement): boolean {
    return element.scrollTop + element.clientHeight === element.scrollHeight;
}

onMounted(() => {
    getContent();
    nextTick(() => {
        let editorElement = editorContainer.value.querySelector('.cm-editor');
        let scrollerElement = editorElement.querySelector('.cm-scroller') as HTMLElement;
        if (scrollerElement) {
            scrollerElement.addEventListener('scroll', function () {
                if (isScrolledToBottom(scrollerElement)) {
                    getContent();
                }
            });
        }
    });
});

onUnmounted(() => {
    onCloseLog();
});
</script>
