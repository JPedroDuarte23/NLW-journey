import { Button } from "@/components/button";
import { Input } from "@/components/input";
import { Modal } from "@/components/modal";
import { ParticipantProps, Participant } from "@/components/participant";
import { TripLinkProps, TripLink } from "@/components/tripLink";
import { linksServer } from "@/server/links-server";
import { participantsServer } from "@/server/participants-server";
import { colors } from "@/styles/colors";
import { validateInput } from "@/utils/validateInput";
import { Plus } from "lucide-react-native";
import { useEffect, useState } from "react";
import { View, Text, Alert, FlatList  } from "react-native";

enum MODAL {
    NONE = 0,
    NEW_LINK = 1
}

export function Details({tripId}: {tripId: string}){

    //LOADING
    const [isCreatingLinkTrip, setIsCreatingLinkTrip] = useState(false)

    //MODAL
    const [showModal, setShowModal] = useState(MODAL.NONE)

    //DATA
    const [linkName, setLinkName] = useState("")
    const [linkUrl, setLinkUrl] = useState("")

    //LISTS
    const [links, setLinks] = useState<TripLinkProps[]>([])
    const [participants, setParticipants] = useState<ParticipantProps[]>([])

    function resetNewLinkFiedls(){
        setLinkName("")
        setLinkUrl("")
        setShowModal(MODAL.NONE)
    }

    async function handleCreateLinkTrip() {
        try {
            if(!linkName.trim()){
                return Alert.alert("Link", "Informe um título para o link")
            }

            if(!validateInput.url(linkUrl.trim())){
                return Alert.alert("Link", "Link inválido")
            }
            
            setIsCreatingLinkTrip(true)

            await linksServer.create({
                tripId: tripId,
                title: linkName,
                url: linkUrl
            })

            Alert.alert("Link", "Link criado com sucesso")
            resetNewLinkFiedls()
            await getTripLinks()
        } catch (error) {
            console.log(error)
        } finally {
            setIsCreatingLinkTrip(false)
        }
    }

    async function getTripLinks() {
        try {
            const links = await linksServer.getLinksByTripId(tripId)
            setLinks(links)
        } catch (error) {
            console.log(error)
        }
    }

    async function getTripParticipants() {
        try {
            const participants = await participantsServer.getByTripId(tripId)

            setParticipants(participants)
        } catch (error) {
            console.log(error)
        }
    }

    useEffect(() => {
        getTripLinks()
        getTripParticipants()
    }, [])

    return(
        <View className="flex-1 mt-10">
            <Text className="text-zinc-50 text-2xl font-semibold mb-2">Links importantes</Text>

            <View className="flex-1">
                {links.length > 0 ? 
                <FlatList 
                data={links} 
                keyExtractor={(item) => item.id} 
                renderItem={({ item }) => <TripLink data={item} />}
                contentContainerClassName="gap-4"
                /> 
            : (
                <Text className="text-zinc-400 font-regular text-base mt-2 mb-6">
                    Nenhum link adicionado
                </Text>
            )
            }
                
                <Button variant="secondary" onPress={() => setShowModal(MODAL.NEW_LINK)}>
                    <Plus color={colors.zinc[200]} size={20}/>
                    <Button.Title>Cadastrar novo link</Button.Title>
                </Button>
            </View>

            <View className="flex-1 border-t border-zinc-800 mt-6">
                <Text className="text-zinc-50 test-2xl font-semibold my-6">
                    Convidados
                </Text>
                {participants.length > 0 ? 
                <FlatList 
                data={participants} 
                keyExtractor={(item) => item.id} 
                renderItem={({ item }) => <Participant data={item} />}
                contentContainerClassName="gap-4 pb-44"
                /> 
            : (
                <Text className="text-zinc-400 font-regular text-base mt-2 mb-6">
                    Nenhum participante adicionado
                </Text>
            )
            }
            </View>

            <Modal
                title="Cadastrar link"
                subtitle="Todos os convidados podem visualizar os links importantes"
                visible={showModal === MODAL.NEW_LINK}
                onClose={() => setShowModal(MODAL.NONE)}
            >
                <View className="gap-2 mb-3">
                    <Input>
                        <Input.Field 
                        placeholder="Título do link"
                        onChangeText={(text) => setLinkName(text)}
                        value={linkName}/>
                    </Input>

                    <Input>
                        <Input.Field 
                        placeholder="URL"
                        onChangeText={(text) => setLinkUrl(text)}
                        value={linkUrl}/>
                    </Input>
                </View>
                <Button onPress={handleCreateLinkTrip} isLoading={isCreatingLinkTrip}>
                    <Button.Title>Salvar link</Button.Title>
                </Button>
            </Modal>
        </View>
    )
}