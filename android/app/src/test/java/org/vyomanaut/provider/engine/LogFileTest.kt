package org.vyomanaut.provider.engine

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import java.io.File

class LogFileTest {
    @get:Rule
    val folder = TemporaryFolder()

    @Test
    fun writtenLinesReadBack() {
        val file = File(folder.root, LogFile.NAME)
        LogFile.write(file, listOf("one", "two", "three"))
        assertEquals(listOf("one", "two", "three"), LogFile.read(file, maxLines = 200))
    }

    @Test
    fun readKeepsOnlyTheNewestLines() {
        val file = File(folder.root, LogFile.NAME)
        LogFile.write(file, (1..10).map { "line $it" })
        assertEquals(listOf("line 9", "line 10"), LogFile.read(file, maxLines = 2))
    }

    @Test
    fun overwritingReplacesTheOldContentAndLeavesNoTempFile() {
        val file = File(folder.root, LogFile.NAME)
        LogFile.write(file, listOf("old"))
        LogFile.write(file, listOf("new"))
        assertEquals(listOf("new"), LogFile.read(file, maxLines = 200))
        assertFalse(File(folder.root, LogFile.NAME + ".tmp").exists())
    }

    @Test
    fun missingFileReadsAsEmpty() {
        assertEquals(emptyList<String>(), LogFile.read(File(folder.root, "absent.txt"), maxLines = 200))
    }
}
